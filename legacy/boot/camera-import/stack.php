<?php
// Stapelt zusammengehörige RAW- und Bild-Dateien in Immich (z.B. DSC01234.ARW
// + DSC01234.HIF), auch für früher hochgeladene Bilder.
//
// Ein Paar entsteht nur, wenn
//   - der Dateiname ohne Endung gleich ist,
//   - die Aufnahmezeiten höchstens STACK_TOLERANCE Sekunden auseinanderliegen,
//   - das Kameramodell gleich ist (wenn beide eins haben),
//   - die Zuordnung eindeutig ist (genau ein Partner auf beiden Seiten),
//   - keine der beiden Dateien schon in einem Stapel liegt.
//
// Aufruf:
//   php stack.php [--since=JJJJ-MM-TT|ISO-Zeit] [--dry-run] [--conf=PFAD] [--status=DATEI]
// Ohne --since wird die ganze Bibliothek durchsucht.
// Letzte Ausgabezeile: "ERGEBNIS paare=N angelegt=N mehrdeutig=N ohnedatum=N fehler=N"

const RAW_EXT = ['arw', 'cr2', 'cr3', 'nef', 'nrw', 'raf', 'dng', 'orf', 'rw2',
                 'pef', 'srw', '3fr', 'iiq', 'erf', 'srf', 'sr2'];
const DISPLAY_EXT = ['hif', 'heif', 'heic', 'jpg', 'jpeg'];

require __DIR__ . '/immich.php';

$opt = getopt('', ['since::', 'dry-run', 'conf::', 'status::']);
$statusFile = $opt['status'] ?? '';
$conf = read_conf($opt['conf'] ?? '/boot/config/camera-import/camera-import.conf');
$url = rtrim($conf['IMMICH_URL'] ?? '', '/');
$key = $conf['IMMICH_API_KEY'] ?? '';
$tolerance = (int)($conf['STACK_TOLERANCE'] ?? 2);
$cover = $conf['STACK_COVER'] ?? 'display';
$dry = isset($opt['dry-run']);
if ($url === '' || $key === '') {
    fwrite(STDERR, "IMMICH_URL oder IMMICH_API_KEY fehlt\n");
    exit(2);
}

// Fortschritt für /status in eine Datei schreiben
function progress(string $text): void {
    global $statusFile;
    if ($statusFile !== '') @file_put_contents($statusFile, $text . "\n");
}

function ext_of(string $name): string {
    return strtolower(pathinfo($name, PATHINFO_EXTENSION));
}

// Nur das von Immich selbst ausgelesene Aufnahmedatum: das Dateidatum weicht bei
// manchen Formaten ab, und vor dem Metadaten-Job von Immich fehlt es noch.
function taken_at(array $a): ?int {
    $t = $a['exifInfo']['dateTimeOriginal'] ?? null;
    return $t ? strtotime($t) : null;
}

function camera(array $a): string {
    return trim(($a['exifInfo']['make'] ?? '') . ' ' . ($a['exifInfo']['model'] ?? ''));
}

// --- Bilder aus Immich holen ------------------------------------------------
$query = ['size' => 1000, 'withExif' => true, 'withStacked' => true];
if (!empty($opt['since'])) {
    $ts = strtotime($opt['since']);
    if ($ts === false) { fwrite(STDERR, "Ungültiges Datum: {$opt['since']}\n"); exit(2); }
    $query['createdAfter'] = gmdate('Y-m-d\TH:i:s.000\Z', $ts);
}

$groups = [];
$seen = 0;
$ids = [];
$page = 1;
while (true) {
    [$status, $res] = api('POST', '/search/metadata', $query);
    if ($status !== 200 || !isset($res['assets']['items'])) {
        fwrite(STDERR, "Suche fehlgeschlagen (HTTP $status): " . json_encode($res) . "\n");
        echo "ERGEBNIS paare=0 angelegt=0 mehrdeutig=0 ohnedatum=0 fehler=1\n";
        exit(1);
    }
    $new = 0;
    foreach ($res['assets']['items'] as $a) {
        if (isset($ids[$a['id']])) continue;
        $ids[$a['id']] = true;
        $new++;
        $seen++;
        if (($a['type'] ?? '') !== 'IMAGE' || !empty($a['isTrashed'])) continue;
        $ext = ext_of($a['originalFileName'] ?? '');
        if (!in_array($ext, RAW_EXT, true) && !in_array($ext, DISPLAY_EXT, true)) continue;
        $base = strtolower(pathinfo($a['originalFileName'], PATHINFO_FILENAME));
        $groups[$base][] = [
            'id' => $a['id'],
            'name' => $a['originalFileName'],
            'raw' => in_array($ext, RAW_EXT, true),
            't' => taken_at($a),
            'cam' => camera($a),
            'stacked' => !empty($a['stack']),
        ];
    }
    // Liefert Immich nur schon Gelesenes, klappt das Blättern nicht: abbrechen
    // statt endlos dieselbe Seite zu holen
    if ($new === 0 && $res['assets']['items']) {
        fwrite(STDERR, "Blättern fehlgeschlagen: Immich liefert dieselben Bilder erneut\n");
        echo "ERGEBNIS paare=0 angelegt=0 mehrdeutig=0 ohnedatum=0 fehler=1\n";
        exit(1);
    }
    // Ab Immich 3.2 per Cursor, davor per Seitenzahl
    if (!empty($res['assets']['nextCursor'])) {
        $query['cursor'] = $res['assets']['nextCursor'];
    } elseif (!empty($res['assets']['nextPage'])) {
        $query['page'] = (int)$res['assets']['nextPage'];
    } else {
        break;
    }
    progress("Bibliothek lesen: $seen Dateien");
    if (++$page % 5 === 0) { echo "… $seen Dateien gelesen\n"; flush(); }
}
echo "$seen Dateien gelesen, " . count($groups) . " Dateinamen\n";

// --- Paare bilden -----------------------------------------------------------
progress("Paare suchen in $seen Dateien …");
function matches(array $raw, array $img, int $tol): bool {
    if ($raw['t'] === null || $img['t'] === null) return false;
    if (abs($raw['t'] - $img['t']) > $tol) return false;
    if ($raw['cam'] !== '' && $img['cam'] !== '' && $raw['cam'] !== $img['cam']) return false;
    return true;
}

$pairs = [];
$ambiguous = 0;
$nodate = 0;
foreach ($groups as $files) {
    $raws = array_values(array_filter($files, fn($f) => $f['raw']));
    $imgs = array_values(array_filter($files, fn($f) => !$f['raw']));
    if (!$raws || !$imgs) continue;
    // Ohne Aufnahmedatum lässt sich nicht sicher paaren; später erneut versuchen
    $undated = array_filter($files, fn($f) => $f['t'] === null && !$f['stacked']);
    if ($undated) { $nodate++; continue; }
    foreach ($raws as $r) {
        $cand = array_values(array_filter($imgs, fn($i) => matches($r, $i, $tolerance)));
        if (count($cand) !== 1) {
            if (count($cand) > 1) $ambiguous++;
            continue;
        }
        $i = $cand[0];
        // Gegenprobe: das Bild darf nur zu diesem einen RAW passen
        $back = array_filter($raws, fn($x) => matches($x, $i, $tolerance));
        if (count($back) !== 1) { $ambiguous++; continue; }
        if ($r['stacked'] || $i['stacked']) continue;
        $pairs[] = $cover === 'raw' ? [$r, $i] : [$i, $r];
    }
}

// --- Stapel anlegen ---------------------------------------------------------
$created = 0;
$errors = 0;
$total = count($pairs);
$t0 = microtime(true);
foreach ($pairs as $n => [$primary, $other]) {
    if (!$dry && $n % 10 === 0) {
        $line = "Stapel anlegen: $n von $total (" . intdiv(100 * $n, max($total, 1)) . " %)";
        if ($n >= 20) {
            $rest = (int)round((microtime(true) - $t0) / $n * ($total - $n) / 60);
            $line .= $rest < 1 ? ", gleich fertig" : ", noch ca. $rest Min.";
        }
        progress($line);
    }
    if ($dry) {
        echo "würde stapeln: {$primary['name']} + {$other['name']}\n";
        continue;
    }
    [$status, $res] = api('POST', '/stacks', ['assetIds' => [$primary['id'], $other['id']]]);
    if ($status >= 200 && $status < 300) {
        $created++;
    } else {
        $errors++;
        if ($errors <= 5) {
            fwrite(STDERR, "Stapel {$primary['name']} fehlgeschlagen (HTTP $status): " . json_encode($res) . "\n");
        }
    }
}

printf("ERGEBNIS paare=%d angelegt=%d mehrdeutig=%d ohnedatum=%d fehler=%d\n",
       count($pairs), $created, $ambiguous, $nodate, $errors);
exit($errors ? 1 : 0);
