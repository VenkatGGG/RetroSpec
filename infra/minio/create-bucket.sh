#!/bin/sh
set -eu

mc alias set local http://minio:9000 minioadmin minioadmin
mc mb --ignore-existing local/retrospec-artifacts
mc anonymous set private local/retrospec-artifacts
