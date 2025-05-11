#!/bin/bash

make

cp -r cmd/lora-simulator ./lora-simulator

rm -f ./lora-simulator/simulator.log
rm -f ./lora-simulator/config/.gitignore
rm -f ./lora-simulator/.gitignore
rm -f ./lora-simulator/main.go
rm -rf ./lora-simulator/cmd

tar -czvf lora-simulator.tar.gz ./lora-simulator

rm -rf ./lora-simulator