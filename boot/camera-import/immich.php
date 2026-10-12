<?php
// Gemeinsame Helfer für stack.php und album.php: Einstellungen lesen, Immich-API.

// Einstellungen im Shell-Format (KEY="wert") lesen
function read_conf(string $path): array {
    $out = [];
    foreach (@file($path) ?: [] as $line) {
        if (preg_match('/^\s*([A-Z_]+)=(["\']?)(.*)\2\s*(#.*)?$/', $line, $m)) {
            $out[$m[1]] = $m[3];
        }
    }
    return $out;
}

function api(string $method, string $path, ?array $body = null): array {
    global $url, $key;
    $http = [
        'method' => $method,
        'header' => "x-api-key: $key\r\nContent-Type: application/json\r\nAccept: application/json\r\n",
        'timeout' => 120,
        'ignore_errors' => true,
    ];
    if ($body !== null) $http['content'] = json_encode($body);
    $raw = @file_get_contents($url . '/api' . $path, false, stream_context_create(['http' => $http]));
    $status = 0;
    foreach ($http_response_header ?? [] as $h) {
        if (preg_match('#^HTTP/\S+\s+(\d+)#', $h, $m)) $status = (int)$m[1];
    }
    return [$status, json_decode($raw ?: 'null', true)];
}
