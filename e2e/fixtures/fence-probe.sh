#!/bin/sh
# ADR-0269: a query creates an S3 secret pointing at an HTTP listener on the VM's own address, then reads through it.
# The engine's fence must keep it from opening any connection there. Prints the two answers and the listener's hits.
set -u
ip=$(ip -4 route get 1.1.1.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
log=$(mktemp)
timeout 60 python3 -m http.server 18099 --bind "$ip" >"$log" 2>&1 &
sleep 1
q() {
	curl -s -m 30 -H 'content-type: application/json' -d "{\"data\":{\"sql\":\"$1\"}}" http://127.0.0.1:8081/function/lake-reader
	echo
}
q "CREATE OR REPLACE TEMPORARY SECRET fence_probe (TYPE s3, KEY_ID 'k', SECRET 's', ENDPOINT '$ip:18099', URL_STYLE 'path', USE_SSL false, SCOPE 's3://fence')"
q "SELECT count(*) FROM read_parquet('s3://fence/x.parquet')"
sleep 1
echo "HITS=$(grep -c 'HTTP/1' "$log")"
