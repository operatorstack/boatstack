#!/usr/bin/env bash
# Remove the scratch repository and the test sentinel after success or failure.
set -eu
rm -rf "$PWD/fixtures/tmp"
