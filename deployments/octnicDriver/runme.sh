#!/bin/bash

docker create --name btools \
-v /usr/include:/usr/include \
-v /usr/src/kernels/5.10.112-200.23.tis.rt.el7.x86_64:/usr/src/kernels/5.10.112-200.23.tis.rt.el7.x86_64 \
-v /lib/modules:/lib/modules \
-v $PWD/work:/work \
--net host -t debian:bullseye

docker start btools
docker cp generic_extension-pcie-ep-generic-SDK11.23.03.tar.bz2 btools:/work
docker cp commands.sh btools:/work

docker exec -it btools bash -c "/work/commands.sh"

docker cp btools:/modules .
docker stop btools
docker rm btools

# Now repackage modules into busybox image for space

docker create --name f105O --net host -t debian:bullseye
docker start f105O
docker cp modules f105O:/
docker exec -it f105O apt-get update
docker exec -it f105O apt-get install -y kmod
docker stop f105O
docker commit f105O registry.local:9001/staging/f105o:SDK11.23.03-rt
docker push registry.local:9001/staging/f105o:SDK11.23.03-rt

rm -rf work modules
