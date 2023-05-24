/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	acclrv1beta1 "github.com/zbsarashki/OctNic/api/v1beta1"

	// To be removed
	"net/http"
)

// OctNicReconciler reconciles a OctNic object
type OctNicReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var DRIVER_DAEMON = "DRIVER_DAEMON"
var MONITR_DAEMON = "MONITR_DAEMON"
var PLUGIN_DAEMON = "PLUGIN_DAEMON"

var MRVL_LABEL_KEY = "marvell.com/inline_acclr_present"
var NODE_LABEL_NAME = "kubernetes.io/hostname"

type stateControl struct {
	drvState bool
	dpState bool

	updState  bool
	monState bool
	confState bool

	uPod corev1.Pod
	mPod corev1.Pod
	cPod corev1.Pod

	drvSet appsv1.DaemonSet
	dpSet appsv1.DaemonSet

	ctx context.Context
	r *OctNicReconciler
	mctx *acclrv1beta1.OctNic

	currentDevState DevState
	desiredDevState DevState
}

type DevState struct {
	PciAddr  string
	Numvfs   string
	FwImage  string
	FwTag    string
	Status   string
	PfDriver string
}

// The remote utlitites included in the tools image must
// query the device for the sofware versions and reflash
// if device's fw version differs from the CRD requested
// version. This is currently not supported by the remote
// utlitities.
func (Z *stateControl) s0_dev_state_check(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) error {

	fmt.Printf("-----> s0_dev_state_check\n")
	rvl := DevState{}

	if Z.monState == false {
		// Monitor Pod does not exist
		podManifest := "/manifests/dev-monitor/" + mctx.Spec.InlineAcclrs[0].Acclr + "-dev-monitor.yaml"
		byf, err := ioutil.ReadFile(podManifest)
		if err != nil {
			fmt.Printf("Manifest %s not found: %s\n", podManifest, err)
			Z.currentDevState = rvl
			return err
		}

		reg, _ := regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
		byf = reg.ReplaceAll(byf, []byte(mctx.Spec.NodeName))
		reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
		byf = reg.ReplaceAll(byf, []byte("nodeName: "+mctx.Spec.NodeName))

		var pciAddr []string
		for _, e := range mctx.Spec.InlineAcclrs {
			pciAddr = append(pciAddr, e.PciAddr)
		}

		reg, _ = regexp.Compile(`value: PCIADDR_FILLED_BY_OPERATOR`)
		byf = reg.ReplaceAll(byf, []byte("value: "+strings.Join(pciAddr[:], ",")))

		p := corev1.Pod{}
		yamlutil.Unmarshal(byf, &p)

		err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			return nil
		}

		err = r.Create(ctx, &p)
		if err != nil {
			fmt.Printf("Failed to create pod: %s\n", err)
			return nil
		}
		fmt.Printf("@ %+v\n", pciAddr[:])
		return nil
	}

	//for _, e := range mctx.Spec.InlineAcclrs {
	e := mctx.Spec.InlineAcclrs[0]
	if Z.mPod.Status.Phase != "Running" {
		// There is at most one monitor pod per node
		// So it is either pending or has crashed
		//fmt.Printf("%s: %s == %s\n",
		//		mPs.Items[0].Status.Phase,
		//		mPs.Items[0].Spec.NodeName,
		//		mctx.Spec.NodeName)
		return nil
	}

	// TODO: This part needs to change
	getUrl := "http://" + Z.mPod.Status.PodIP + ":4004/" + e.PciAddr + "/status.yml"
	resp, err := http.Get(getUrl)
	if err != nil {
		fmt.Printf("http.Get Failed: %s\n", err)
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	err = yamlutil.Unmarshal(data, &rvl)
	if err != nil {
		fmt.Printf("Unmarshal Failed: %s\n", err)
		return nil
	}
	fmt.Printf("-----> %d, %+v\n", len(data), rvl)
	//}

	Z.currentDevState.PciAddr = rvl.PciAddr
	Z.currentDevState.Numvfs= rvl.Numvfs
	Z.currentDevState.FwImage = rvl.FwImage
	Z.currentDevState.FwTag = rvl.FwTag
	Z.currentDevState.Status = rvl.Status
	Z.currentDevState.PfDriver = rvl.PfDriver
	fmt.Printf("-----> %d, %+v\n", len(data), rvl)
	fmt.Printf("Z----> %d, %+v\n", len(data), Z.currentDevState)
	return nil
}

func (Z *stateControl) s1_device_remove(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) error {
	// We are removing an initialized device.
	fmt.Printf("s1_device_remove: Re/Start validator Pod\n")

	if Z.updState == true {
		fmt.Printf("Current update running Return nil from remove\n")
		return nil
	}

	unbind_pci_address := mctx.Spec.InlineAcclrs[0].PciAddr
	if unbind_pci_address != Z.currentDevState.PciAddr {
		fmt.Printf("Rejecting unexpected device remove attempt!\n")
		return nil
	}

	if Z.confState == true {
		if Z.cPod.Status.Phase == "Succeeded" {
			// Delete it.
			err := r.Delete(ctx, &Z.cPod)
			return err
		}
		return nil
	}

	// TODO:
	// Handle case when the pod is pending or running

	fmt.Printf("Re/Start validator Pod\n")

	podManifest := "/manifests/drv-daemon-validate/" + mctx.Spec.InlineAcclrs[0].Acclr + "-drv-validate.yaml"
	p := corev1.Pod{}

	byf, err := ioutil.ReadFile(podManifest)
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}

	reg, _ := regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte(mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("nodeName: "+mctx.Spec.NodeName))

	reg, _ = regexp.Compile(`value: NUMVFS_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].NumVfs))
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	// TODO: handle multiple devices
	// The device configuration must be able to handle multiple devices
	reg, _ = regexp.Compile(`value: PCIADDR_UNBIND_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+unbind_pci_address))

	//bind_pci_address :=
	//reg, _ = regexp.Compile(`value: PCIADDR_BIND_FILLED_BY_OPERATOR`)
	//byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	reg, _ = regexp.Compile(`value: PREFIX_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].ResourcePrefix))
	reg, _ = regexp.Compile(`value: RESOURCENAMES_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+
		strings.Join(mctx.Spec.InlineAcclrs[0].ResourceName[:], ",")))

	yamlutil.Unmarshal(byf, &p)
	err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return err
	}
	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return err
	}

	return nil
}

func (Z *stateControl) s3_device_add(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) (err error) {
	fmt.Printf("s3_device_add: Re/Start validator Pod\n")

	if Z.updState == true {
		fmt.Printf("Current update running Return nil from remove\n")
		return nil
	}

	if Z.confState == true {
		if Z.cPod.Status.Phase == "Succeeded" {
			// Delete it.
			err := r.Delete(ctx, &Z.cPod)
			return err
		}
		return nil
	}

	podManifest := "/manifests/drv-daemon-validate/" + mctx.Spec.InlineAcclrs[0].Acclr + "-drv-validate.yaml"
	p := corev1.Pod{}
	byf, err := ioutil.ReadFile(podManifest)
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}

	reg, _ := regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte(mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("nodeName: "+mctx.Spec.NodeName))

	reg, _ = regexp.Compile(`value: NUMVFS_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].NumVfs))
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	// TODO: handle multiple devices
	// The device configuration must be able to handle multiple devices
	//unbind_pci_address :=
	//reg, _ = regexp.Compile(`value: PCIADDR_UNBIND_FILLED_BY_OPERATOR`)
	//byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	//bind_pci_address :=
	reg, _ = regexp.Compile(`value: PCIADDR_BIND_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	reg, _ = regexp.Compile(`value: PREFIX_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].ResourcePrefix))
	reg, _ = regexp.Compile(`value: RESOURCENAMES_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+
		strings.Join(mctx.Spec.InlineAcclrs[0].ResourceName[:], ",")))

	yamlutil.Unmarshal(byf, &p)
	err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return err
	}
	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return err
	}

	return err
}

func (Z *stateControl) s1_device_reflash(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) (err error) {
	fmt.Printf("s1_device_reflash\n")

	if Z.updState == true {
		if Z.uPod.Status.Phase == "Succeeded" {
			r.Delete(ctx, &Z.cPod)
		}
		return nil
	}

	p := corev1.Pod{}
	l := mctx.Spec.InlineAcclrs[0].Acclr
	byf, err := ioutil.ReadFile("/manifests/dev-update/" + l + "-update.yaml")
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}

	reg, _ := regexp.Compile(`image: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("image: "+mctx.Spec.InlineAcclrs[0].FwImage+":"+mctx.Spec.InlineAcclrs[0].FwTag))
	reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("nodeName: "+mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte(mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`value: PCIADDR_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.InlineAcclrs[0].PciAddr))

	err = yamlutil.Unmarshal(byf, &p)
	if err != nil {
		fmt.Printf("%s\n", err)
		return err
	}
	err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return err
	}

	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return err
	}

	return err
}

func (Z *stateControl) state_check(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) error {

	fmt.Printf("Kstate_check\n")

	daemSet := appsv1.DaemonSet{}

	// Driver daemonset running?
	daemonName := mctx.Spec.InlineAcclrs[0].Acclr + "-driver"
	err := r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: mctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.drvState = false
	} else {
		Z.drvState = true
		Z.drvSet = daemSet
	}

	// DevicePlugin daemonset running?
	daemonName = mctx.Spec.InlineAcclrs[0].Acclr + "-sriovdp"
	err = r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: mctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.dpState = false
	} else {
		Z.dpState = true
		Z.dpSet = daemSet
	}

	// Is monitor Pod running
	mPs := &corev1.PodList{}
	l := mctx.Spec.InlineAcclrs[0].Acclr
	r.List(ctx, mPs, client.MatchingLabels{"app": l + "-dev-monitor"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName},
	)
	if len(mPs.Items) == 0 {
		Z.monState = false
	} else {
		Z.monState = true
		Z.mPod = mPs.Items[0]
	}

	// Is reflashPod running?
	l = mctx.Spec.InlineAcclrs[0].Acclr
	r.List(ctx, mPs, client.MatchingLabels{"app": l + "-dev-update"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName},
	)
	if len(mPs.Items) == 0 {
		Z.updState = false
	} else {
		Z.updState = true
		Z.uPod = mPs.Items[0]
	}

	// Is confPod running?
	l = mctx.Spec.InlineAcclrs[0].Acclr
	r.List(ctx, mPs, client.MatchingLabels{"app": l + "-drv-validate"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName},
	)
	if len(mPs.Items) == 0 {
		Z.confState = false
	} else {
		Z.confState = true
		Z.cPod = mPs.Items[0]
	}

	return nil
}

func (Z *stateControl) init_rest(ctx context.Context, r *OctNicReconciler, mctx *acclrv1beta1.OctNic) error {
	fmt.Printf("init_rest\n")

	// Start driver dameonset
	daemonManifest := "/manifests/drv-daemon/" + mctx.Spec.InlineAcclrs[0].Acclr + "-drv.yaml"

	p := appsv1.DaemonSet{}
	byf, err := ioutil.ReadFile(daemonManifest)
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}
	yamlutil.Unmarshal(byf, &p)
	err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return err
	}
	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return err
	}

	// Start Device plugin set
	daemonManifest = "/manifests/dev-plugin/" + mctx.Spec.InlineAcclrs[0].Acclr + "-sriovdp.yaml"

	p = appsv1.DaemonSet{}
	byf, err = ioutil.ReadFile(daemonManifest)
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}
	yamlutil.Unmarshal(byf, &p)
	err = ctrl.SetControllerReference(mctx, &p, r.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return err
	}
	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return err
	}
	return nil
}

//+kubebuilder:rbac:groups=acclr.github.com,resources=octnics,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=acclr.github.com,resources=octnics/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=acclr.github.com,resources=octnics/finalizers,verbs=update

//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the OctNic object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile

func (r *OctNicReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	fmt.Printf("Reconcile reached\n")
	mctx := &acclrv1beta1.OctNic{}
	if err := r.Get(ctx, req.NamespacedName, mctx); err != nil {
		fmt.Printf("unable to fetch OctNic Policy: %s\n", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	fmt.Printf("vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n")

	Z := stateControl{}
	Z.ctx = ctx
	Z.r = r
	Z.mctx = mctx

	// Read device status from device
	Z.state_check(ctx, r, mctx)
	fmt.Printf("-----> %+v\n", Z.currentDevState)
	Z.s0_dev_state_check(ctx, r, mctx)
	fmt.Printf("-----> %+v\n", Z.currentDevState)

	var err error
	if Z.drvState == false || Z.dpState == false {
		err = Z.init_rest(ctx, r, mctx)
		return ctrl.Result{}, err
	}
	a := mctx.Spec.InlineAcclrs[0]
	fmt.Printf("pci %s == ? %s\n", Z.currentDevState.PciAddr, a.PciAddr)
	if Z.currentDevState.PciAddr == a.PciAddr {
		if (Z.currentDevState.FwImage != a.FwImage) || 
			  (Z.currentDevState.FwTag != a.FwTag) {
				if Z.currentDevState.PfDriver != "" {
					err := Z.s1_device_remove(ctx, r, mctx)
					return ctrl.Result{}, err
				} else {
					err := Z.s1_device_reflash(ctx, r, mctx)
					return ctrl.Result{}, err
				}
			}
	}
	if Z.confState == false {
		if Z.drvState == true && Z.dpState == true {
			err := Z.s3_device_add(ctx, r, mctx)
			return ctrl.Result{}, err
		}
	}
		
	fmt.Printf("vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n")
	fmt.Printf("node: %s\n", mctx.Spec.NodeName)
	fmt.Printf("NumAcclr: %d\n", len(mctx.Spec.InlineAcclrs))
	fmt.Printf("^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\n")

	return ctrl.Result{}, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *OctNicReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}); err != nil {
		return err
	}

	c, err := controller.New("OctNic-controller", mgr,
		controller.Options{Reconciler: r, MaxConcurrentReconciles: 1})
	if err != nil {
		fmt.Printf("Failed to create new controller: %s\n", err)
		return err
	}
	err = c.Watch(&source.Kind{Type: &acclrv1beta1.OctNic{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		fmt.Printf("Failed to add watch controller: %s\n", err)
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &acclrv1beta1.OctNic{},
	})
	if err != nil {
		fmt.Printf("Failed to add watch controller: %s\n", err)
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.DaemonSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &acclrv1beta1.OctNic{},
	})
	if err != nil {
		fmt.Printf("Failed to add watch controller: %s\n", err)
		return err
	}
	return nil
}
