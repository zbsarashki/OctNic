#!/bin/bash

docker create --name btools \
-v /usr/lib/linux-rt-kbuild-5.10:/usr/lib/linux-rt-kbuild-5.10 \
-v /usr/src/linux-rt-headers-5.10.0-6-rt-common:/usr/src/linux-rt-headers-5.10.0-6-rt-common \
-v /usr/src/linux-rt-headers-5.10.0-6-rt-amd64:/usr/src/linux-rt-headers-5.10.0-6-rt-amd64 \
-v /usr/src/kernels/5.10.112-200.23.tis.rt.el7.x86_64:/usr/src/kernels/5.10.112-200.23.tis.rt.el7.x86_64 \
-v /lib/modules:/lib/modules \
-v $PWD/work:/work \
--net host -t debian:bullseye

docker start btools
docker cp generic_extension-pcie-ep-generic-SDK11.22.11.tar.bz2 btools:/work
docker cp commands.sh btools:/work

docker exec -it btools bash -c "/work/commands.sh"

docker cp btools:/modules .
docker stop btools
docker rm btools

# Now repackage modules into busybox image for space

docker create --name f95oD --net host -t debian:bullseye
docker start f95oD
docker cp modules f95oD:/
docker exec -it f95oD apt-get update
docker exec -it f95oD apt-get install -y kmod
docker stop f95oD
docker commit f95oD registry.local:9001/staging/f95o:legacy-rt
docker push registry.local:9001/staging/f95o:legacy-rt

rm -rf work modules
