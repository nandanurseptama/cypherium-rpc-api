#!/bin/bash

cp -rf ./../../crypto/bls/lib/mac/*     ./../../crypto/bls/lib/
cp -rf ./../../pow/cphash/randomX/lib/Darwin/*   ./../../pow/cphash/randomX/
go build ./rpcClient.go

cp -rf ./../../crypto/bls/lib/linux/*     ./../../crypto/bls/lib/
cp -rf ./../../pow/cphash/randomX/lib/Linux/*   ./../../pow/cphash/randomX/
