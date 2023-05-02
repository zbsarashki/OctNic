#!/bin/bash


apt-get update
apt-get install build-essential wget curl vim unzip libelf1 libssl1.1 linux-compiler-gcc-10-x86 bc kmod

cd /work
tar -xvjpf generic_extension-pcie-ep-generic-SDK11.22.11.tar.bz2 \
  extensions-sources-pcie_ep_octeontx-SDK11.22.11/pcie_ep_octeontx/sources-pcie_ep_octeontx-SDK11.22.11.tar.bz2

tar -xvjpf extensions-sources-pcie_ep_octeontx-SDK11.22.11/pcie_ep_octeontx/sources-pcie_ep_octeontx-SDK11.22.11.tar.bz2

cd /work/pcie_ep_octeontx-SDK11.22.11/host
make
make COMPILEFOR=OCTEON_VF

mkdir /modules
for f in $(find ./ -type f -name '*\.ko'); do cp $f /modules/$(basename $f); done
