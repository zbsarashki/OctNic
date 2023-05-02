# Build the controller on a machine other than the controller 
# and transfer to the target

docker build -t registry.local:9001/staging/octnicupdater:0.0.1 -f Dockerfile .
docker save registry.local:9001/staging/octnicupdater:0.0.1 -o octnicupdater.tar
scp octnicupdater.tar sysadmin@controller-0:~/
ssh sysadmin@controller-0
sudo bash
docker load -i octnicupdater.tar
docker login registry.local:9001
docker push registry.local:9001/staging/octnicupdater:0.0.1

# Build the setup-validate pod. Perform these steps on the controller

ssh sysadmin@controller-0
sudo bash
cd octnic-controller/deployments/setup-validate
./runme.sh
exit #sudo

# Build the driver pod for f95o. Perform these steps on the controller

scp generic_extension-pcie-ep-generic-SDK11.22.11.tar.bz2 sysadmin@controller-0~/

ssh sysadmin@controller-0
sudo bash
mv generic*SDK11.22.11*bz2 octnic-controller/deployments/octnicDriver
cd octnic-controller/deployments/octnicDriver
./runme.sh
exit #sudo

# Validate that images are present:

[sysadmin@controller-0 octnicDriver(keystone_admin)]$ system registry-image-list | grep staging
| staging/f950-setup-validate                              |
| staging/f95o                                             |
| staging/octnicupdater                                    |

# Create secrets and Patch default user account

kubectl create secret docker-registry local-registry -n default \
    --docker-server=registry.local:9001 --docker-username=admin \
    --docker-password='<sysdmin Password>' \
    --docker-email=noreply@registry.local
kubectl patch serviceaccount default -p '{"imagePullSecrets": [{"name": "local-registry"}]}'

# Start the chart:

Label host:
system host-label-assign controller-0 marvell.com/inline_acclr_present=true

Install the chart:
helm install o oceonNic

Wait for this condition to occur:

default        f95o-driver-rcmjf                                   1/1     Running     0          86s

default        f95o-drv-validate-controller-0                      0/1     Completed   0          84s

default        f95o-sriovdp-gwhlv                                  1/1     Running     0          86s

default        octnicupdater-controller-manager-844db46d5c-xzn2n   2/2     Running     0          104s


Check the logs:
[sysadmin@controller-0 deployments(keystone_admin)]$ kubectl get nodes controller-0 -o custom-columns=:.status.allocatable | xargs -n1

map[cpu:68

ephemeral-storage:9391196145

hugepages-1Gi:10Gi

hugepages-2Mi:0

marvell.com/marvell_sriov_net_rmp:3

marvell.com/marvell_sriov_net_vamp:4

memory:374498376Ki

pods:110]

[sysadmin@controller-0 deployments(keystone_admin)]$ kubectl logs -f octnicupdater-controller-manager-844db46d5c-ztxbz 

1.681438303683814e+09   INFO    controller-runtime.metrics      Metrics server is starting to listen    {"addr": "127.0.0.1:8080"}

1.6814383036841245e+09  INFO    setup   starting manager

1.6814383036990705e+09  INFO    Starting server {"path": "/metrics", "kind": "metrics", "addr": "127.0.0.1:8080"}

1.6814383036990826e+09  INFO    Starting server {"kind": "health probe", "addr": "[::]:8081"}

I0414 02:11:43.799126       1 leaderelection.go:248] attempting to acquire leader lease default/d4a5ba56.github.com...

I0414 02:12:02.257556       1 leaderelection.go:258] successfully acquired lease default/d4a5ba56.github.com

1.681438322257697e+09   INFO    Starting EventSource    {"controller": "OctNicUpdater-controller", "source": "kind source: *v1beta1.OctNicUpdater"}

1.6814383222577372e+09  INFO    Starting EventSource    {"controller": "OctNicUpdater-controller", "source": "kind source: *v1.Pod"}

1.6814383222577426e+09  INFO    Starting EventSource    {"controller": "OctNicUpdater-controller", "source": "kind source: *v1.DaemonSet"}

1.6814383222577457e+09  INFO    Starting Controller     {"controller": "OctNicUpdater-controller"}

1.6814383222576325e+09  DEBUG   events  octnicupdater-controller-manager-844db46d5c-ztxbz_dc229e41-1cbc-460e-abb1-7a6f04521067 became leader    {"type": "Normal", "object": {"kind":"Lease","namespace":"default","name":"d4a5ba56.github.com","uid":"a62b1f51-7504-4fa7-8542-60c01f4bd207","apiVersion":"coordination.k8s.io/v1","resourceVersion":"100772"}, "reason": "LeaderElection"}

1.6814383223583872e+09  INFO    Starting workers        {"controller": "OctNicUpdater-controller", "worker count": 1}

Start validator Pod

++++++++++++++++++++

numvfs: 7

pciAddr: 0000:af:00.0

prefix: marvell.com

prefix: marvell_sriov_net_vamp#0-3,Octeon_vf,marvell_sriov_net_rmp#4,5-6,Octeon_vf

--------------------


Check resources on the node:

[sysadmin@controller-0 deployments(keystone_admin)]$ kubectl get nodes controller-0 -o custom-columns=:.status.allocatable | xargs -n1

map[cpu:68

ephemeral-storage:9391196145

hugepages-1Gi:10Gi

hugepages-2Mi:0

marvell.com/marvell_sriov_net_rmp:3

marvell.com/marvell_sriov_net_vamp:4

memory:374498376Ki

pods:110]

[sysadmin@controller-0 deployments(keystone_admin)]$ 




