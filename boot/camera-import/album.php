<?php
// Legt nur die bei diesem Import neu hochgeladenen Dateien in ein Album.
// immich-go --into-album würde auch Dateien einsortieren, die schon vorher in
// Immich lagen (Duplikate), deshalb erledigt das dieser Schritt danach.
//
// Neu = in Immich angelegt seit Importbeginn UND Dateiname kommt auf der Karte vor
// (so landen gleichzeitige Uploads vom Handy nicht im Album).
//
// Aufruf:
//   php album.php --name=ALBUM --since=@UNIXZEIT --files=LISTE [--conf=PFAD]
// LISTE: Datei mit einem Dateinamen pro Zeile.
// Letzte Ausgabezeile: "ERGEBNIS neu=N fehler=N"

require __DIR__ . '/immich.php';

$opt = getopt('', ['name:', 'since:', 'files:', 'conf::']);
$conf = read_conf($opt['conf'] ?? '/boot/config/camera-import/camera-import.conf');
$url = rtrim($conf['IMMICH_URL'] ?? '', '/');
$key = $conf['IMMICH_API_KEY'] ?? '';
$name = $opt['name'] ?? '';
$ts = strtotime($opt['since'] ?? '');
if ($url === '' || $key === '' || $name === '' || $ts === false || empty($opt['files'])) {
    fwrite(STDERR, "Aufruf: album.php --name=ALBUM --since=@UNIXZEIT --files=LISTE\n");
    echo "ERGEBNIS neu=0 fehler=1\n";
    exit(2);
}

function fail(string $msg): void {
    fwrite(STDERR, "$msg\n");
    echo "ERGEBNIS neu=0 fehler=1\n";
    exit(1);
}

$names = [];
foreach (@file($opt['files'], FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) ?: [] as $f) {
    $names[strtolower(basename($f))] = true;
}

// --- Neue Dateien suchen ----------------------------------------------------
$query = ['size' => 1000, 'withStacked' => true,
          'createdAfter' => gmdate('Y-m-d\TH:i:s.000\Z', $ts)];
$ids = [];
$seen = [];
while (true) {
    [$status, $res] = api('POST', '/search/metadata', $query);
    if ($status !== 200 || !isset($res['assets']['items'])) {
        fail("Suche fehlgeschlagen (HTTP $status): " . json_encode($res));
    }
    $new = 0;
    foreach ($res['assets']['items'] as $a) {
        if (isset($seen[$a['id']])) continue;
        $seen[$a['id']] = true;
        $new++;
        if (!empty($a['isTrashed'])) continue;
        if (!isset($names[strtolower($a['originalFileName'] ?? '')])) continue;
        $ids[$a['id']] = true;
    }
    // Liefert Immich nur schon Gelesenes, klappt das Blättern nicht
    if ($new === 0) break;
    if (!empty($res['assets']['nextCursor'])) {
        $query['cursor'] = $res['assets']['nextCursor'];
    } elseif (!empty($res['assets']['nextPage'])) {
        $query['page'] = (int)$res['assets']['nextPage'];
    } else {
        break;
    }
}
$ids = array_keys($ids);
echo count($ids) . " neue Dateien für Album \"$name\"\n";
if (!$ids) {
    echo "ERGEBNIS neu=0 fehler=0\n";
    exit(0);
}

// --- Album suchen oder anlegen ----------------------------------------------
[$status, $albums] = api('GET', '/albums');
if ($status !== 200 || !is_array($albums)) fail("Alben lesen fehlgeschlagen (HTTP $status)");
$album = null;
foreach ($albums as $a) {
    if (($a['albumName'] ?? '') === $name) { $album = $a['id']; break; }
}
if ($album === null) {
    [$status, $res] = api('POST', '/albums', ['albumName' => $name]);
    if ($status < 200 || $status >= 300 || empty($res['id'])) {
        fail("Album anlegen fehlgeschlagen (HTTP $status): " . json_encode($res));
    }
    $album = $res['id'];
    echo "Album \"$name\" angelegt\n";
}

// --- Dateien hinzufügen -----------------------------------------------------
$errors = 0;
foreach (array_chunk($ids, 500) as $chunk) {
    [$status, $res] = api('PUT', "/albums/$album/assets", ['ids' => $chunk]);
    if ($status < 200 || $status >= 300) {
        $errors++;
        fwrite(STDERR, "Hinzufügen fehlgeschlagen (HTTP $status): " . json_encode($res) . "\n");
    }
}
printf("ERGEBNIS neu=%d fehler=%d\n", $errors ? 0 : count($ids), $errors);
exit($errors ? 1 : 0);
