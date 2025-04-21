#!/bin/bash

make

cp -r cmd/lora-simulator ./lora-simulator

rm -f ./lora-simulator/simulator.log
rm -f ./lora-simulator/config/.gitignore
rm -f ./lora-simulator/.gitignore

tar -czvf lora-simulator.tar.gz ./lora-simulator
