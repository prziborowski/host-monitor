#!/bin/bash -ex

cd "$(dirname "$0")"

go build
./host-monitor
