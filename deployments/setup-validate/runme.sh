#!/bin/bash

docker create --name vtools --net host -t debian:bullseye
docker start vtools
docker cp config_crd_to_json_v1.sh vtools:/
docker exec vtools apt-get update
docker exec vtools apt-get install -y bash
docker exec vtools chmod 755 config_crd_to_json_v1.sh
docker commit  vtools registry.local:9001/staging/f950-setup-validate:legacy-rt
docker push registry.local:9001/staging/f950-setup-validate:legacy-rt

