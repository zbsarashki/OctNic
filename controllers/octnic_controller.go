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
	//	"io"
	"io/ioutil"
	"regexp"
	//	"strings"

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
)

const (
	S0DaemsStart    = 0
	S0CurNodeState  = 1
	S1SetNextState  = 2
	S2ReFlashDevice = 3
	S2RemoveDevice  = 4
	S2AddDevice     = 5
	S3RestartDP     = 6 // After S2RemoveDevice or S2AddDevice
	SnFinal         = 7
)

// OctNicReconciler reconciles a OctNic object
type OctNicReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var DRIVER_DAEMON = "DRIVER_DAEMON"
var CONTRL_DAEMON = "CONTRL_DAEMON"
var PLUGIN_DAEMON = "PLUGIN_DAEMON"

var MRVL_LABEL_KEY = "marvell.com/inline_acclr_present"
var NODE_LABEL_NAME = "kubernetes.io/hostname"

// type StateFunction func(context.Context, *OctNicReconciler, *acclrv1beta1.OctNic) (ctrl.Result, error)
type StateFunction func() (ctrl.Result, error)
type StateIf interface {
	InitAndExecute(context.Context, *OctNicReconciler, *acclrv1beta1.OctNic) (ctrl.Result, error)
	//run()
	// step()
	// next()
}

type StateControl struct {
	c          AcclrNodeState
	idx        int
	controller []StateFunction

	ctx  context.Context
	rec  *OctNicReconciler
	dctx *acclrv1beta1.OctNic
}

type AcclrNodeState struct {
	drvState  bool
	dpState   bool
	dctrState bool

	updState  bool
	updDevice string
	nodeName  string
	axSet     []AxSet
}

type AxSet struct {
	PfDriver string
	Status   string
	ax       acclrv1beta1.InlineAcclr
}

type DevState struct {
	//inlineAcclrs	acclrv1bet1.InlineAcclr
	PciAddr        string   `json:"pciAddr,omitempty"`
	NumVfs         string   `json:"numvfs,omitempty"`
	FwImage        string   `json:"fwImage,omitempty"`
	FwTag          string   `json:"fwTag,omitempty"`
	ResourcePrefix string   `json:"resourcePrefix,omitempty"`
	ResourceName   []string `json:"resourceName,omitempty"`

	Status   string `json:"status,omitempty"`
	PfDriver string `json:"pfdriver,omitempty"`
	Command  string `json:"command,omitempty"`
}


func (Z *StateControl) podStateRestart(app string) (bool){

	sPs := corev1.PodList{}
	l := Z.dctx.Spec.InlineAcclrs[0].Acclr
	err := Z.rec.List(Z.ctx, &sPs, client.MatchingLabels{"app": l + app},
		client.MatchingFields{"spec.nodeName": Z.dctx.Spec.NodeName},)
	if err !=nil {
		fmt.Printf("Failed to get Pod\n")
		return false
	}
	if len(sPs.Items) == 0 {
		fmt.Printf("Plugin Pod Not found on: %s\n", Z.dctx.Spec.NodeName)
		return false
	}

	err = Z.rec.Delete(Z.ctx, &sPs.Items[0])
	if err !=nil {
		fmt.Printf("Failed to delete Pod\n")
		return false
	}

	return true
}

func (Z *StateControl) init() {
	//fmt.Printf("-->     init()\n")

	if Z.controller != nil {
		return
	}

	Z.controller = make([]StateFunction, SnFinal+2, SnFinal+2)
	Z.controller[S0CurNodeState] = Z.s0CurNodeState
	Z.controller[S0DaemsStart] = Z.s0DaemsStart
	Z.controller[S1SetNextState] = Z.s1SetNextState
	Z.controller[S2AddDevice] = Z.s2AddDevice
	Z.controller[S2RemoveDevice] = Z.s2RemoveDevice
	Z.controller[S2ReFlashDevice] = Z.s2ReFlashDevice
	Z.controller[S3RestartDP] = Z.s3RestartDP
	Z.controller[SnFinal] = Z.snFinal
	Z.controller[SnFinal+1] = nil
}

func (Z *StateControl) InitAndExecute(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     InitAndExecute()\n")
	rvl := 1 //  S0CurNodeState
	Z.ctx = ctx
	Z.dctx = dctx
	Z.rec = r
	Z.init()
	//fmt.Printf("%d\n", len(Z.controller))

	daemSet := appsv1.DaemonSet{}

	daemonName := dctx.Spec.InlineAcclrs[0].Acclr + "-driver"
	err := r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: dctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.c.drvState = false
		rvl = S0DaemsStart
	} else {
		Z.c.drvState = true
	}

	daemonName = dctx.Spec.InlineAcclrs[0].Acclr + "-sriovdp"
	err = r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: dctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.c.dpState = false
		rvl = S0DaemsStart
	} else {
		Z.c.dpState = true
	}

	daemonName = dctx.Spec.InlineAcclrs[0].Acclr + "-dev-control"
	err = r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: dctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.c.dctrState = false
		rvl = S0DaemsStart
	} else {
		Z.c.dctrState = true
	}

	Z.idx = rvl // S0DaemsStart OR S0CurNodeState

	//fmt.Printf("-->     InitAndExecute() %d\n", Z.idx)
	// Loop
	var m ctrl.Result
	for fp := Z.controller[Z.idx]; fp != nil && Z.idx < SnFinal; fp = Z.controller[Z.idx] {

		fmt.Printf("-->     InitAndExecute() %d\n", Z.idx)
		m, err = fp()
		if err != nil {
			fmt.Printf("loop: %s\n", err)
			Z.controller[SnFinal]()
			return m, err
		}
	}
	fmt.Printf("-->     InitAndExecute() Exit\n")
	return ctrl.Result{}, nil
}

func (Z *StateControl) s0StartOneDaem(daemonManifest string) (ctrl.Result, error) {
	p := appsv1.DaemonSet{}
	byf, err := ioutil.ReadFile(daemonManifest)
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		Z.idx = SnFinal
		return ctrl.Result{}, err
	}
	yamlutil.Unmarshal(byf, &p)
	err = ctrl.SetControllerReference(Z.dctx, &p, Z.rec.Scheme)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = Z.rec.Create(Z.ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		Z.idx = SnFinal
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (Z *StateControl) s0DaemsStart() (ctrl.Result, error) {

	fmt.Printf("-->     s0DaemsStart()\n")

	// TODO:
	// Start driver dameonset
	if Z.c.drvState == false {
		daemonManifest := "/manifests/drv-daemon/" + Z.dctx.Spec.InlineAcclrs[0].Acclr + "-drv.yaml"
		m, err := Z.s0StartOneDaem(daemonManifest)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return m, err
		}
	}

	// Start Device plugin set
	if Z.c.dpState == false {
		daemonManifest := "/manifests/dev-plugin/" + Z.dctx.Spec.InlineAcclrs[0].Acclr + "-sriovdp.yaml"
		m, err := Z.s0StartOneDaem(daemonManifest)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return m, err
		}
	}

	// Start device control / monitor set
	if Z.c.dctrState == false {
		daemonManifest := "/manifests/dev-control/" + Z.dctx.Spec.InlineAcclrs[0].Acclr + "-dev-control.yaml"
		m, err := Z.s0StartOneDaem(daemonManifest)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return m, err
		}
	}

	Z.idx = S0CurNodeState
	return ctrl.Result{}, nil
}

func (Z *StateControl) s0CurNodeState() (ctrl.Result, error) {

	fmt.Printf("-->     s0CurNodeState()\n")

	mPs := &corev1.PodList{}
	l := Z.dctx.Spec.InlineAcclrs[0].Acclr
	Z.rec.List(Z.ctx, mPs, client.MatchingLabels{"app": l + "-dev-control"},
		client.MatchingFields{"spec.nodeName": Z.dctx.Spec.NodeName},
	)
	if len(mPs.Items) == 0 {
		fmt.Printf("Control Pod Not found on: %s\n", Z.dctx.Spec.NodeName)
		Z.idx = SnFinal
		//return ctrl.Result{}, nil
		return ctrl.Result{RequeueAfter: 60}, nil
	}

	// TODO: This part needs to change
	//e := Z.dctx.Spec.InlineAcclrs[0]
	//devstate, err := getAcclrState(mPs.Items[0].Status.PodIP, e.PciAddr)
	devState := Z.mkDevState("/StatusDevice")
	devState, err := postAcclr(mPs.Items[0].Status.PodIP, devState)
	if err != nil {
		fmt.Printf("Failed getAcclrState : %s\n", err)
		Z.idx = SnFinal
		//return ctrl.Result{}, nil
		return ctrl.Result{RequeueAfter: 60}, nil
	}

	rvl := acclrv1beta1.InlineAcclr{}
	rvl.PciAddr = devState.PciAddr
	rvl.NumVfs = devState.NumVfs
	rvl.FwImage = devState.FwImage
	rvl.FwTag = devState.FwTag

	Z.c.axSet = append(Z.c.axSet, AxSet{
		PfDriver: devState.PfDriver,
		Status:   devState.Status,
		ax:       rvl})

	Z.idx = S1SetNextState
	return ctrl.Result{}, nil
}

func (Z *StateControl) s1SetNextState() (ctrl.Result, error) {

	fmt.Printf("-->     s1SetNextState()\n")

	c := Z.c.axSet[0]
	d := Z.dctx.Spec.InlineAcclrs[0]

	mPs := &corev1.PodList{}
	l := Z.dctx.Spec.InlineAcclrs[0].Acclr
	Z.rec.List(Z.ctx, mPs, client.MatchingLabels{"app": l + "-dev-update"},
		client.MatchingFields{"spec.nodeName": Z.dctx.Spec.NodeName},
	)
	if len(mPs.Items) != 0 {
		// Add device if reflashing is done
		if mPs.Items[0].Status.Phase == "Succeeded" {
			err := Z.rec.Delete(Z.ctx, &mPs.Items[0])
			if err != nil {
				fmt.Printf("Failed to delete Pod: %s\n", err)
				Z.idx = SnFinal
				return ctrl.Result{}, nil
			}
			Z.idx = S2AddDevice
			return ctrl.Result{}, nil
		} // else continue
		// TODO: Handle fail cases
		Z.idx = SnFinal
		return ctrl.Result{}, nil
		//return ctrl.Result{RequeueAfter: 60}, nil
	}

	if d.PciAddr == c.ax.PciAddr {
		if (d.FwImage != c.ax.FwImage) || (d.FwTag != c.ax.FwTag) {
			if (c.Status == "Linux") || (c.PfDriver != "") {
				Z.idx = S2RemoveDevice
				return ctrl.Result{}, nil
			}
			Z.idx = S2ReFlashDevice
			return ctrl.Result{}, nil
		}
		// Is device status is not Linux then reschedule
		if c.Status != "Linux" {
			Z.idx = SnFinal
			return ctrl.Result{}, nil
			//return ctrl.Result{RequeueAfter: 60}, nil
		}

		if c.ax.NumVfs != d.NumVfs {
			// TODO:
			// - query Device Plugin on the existence of the resources.
			// - ensure correct bindings on the VFs and modules
			Z.idx = S2AddDevice
			return ctrl.Result{}, nil
		}

		fmt.Printf("TODO: REstart DevPlugin Pod on the node\n");
		return ctrl.Result{RequeueAfter: 60}, nil
		// wished we had a since!
		//if c.ax.NumVfs == d.NumVfs && // Therfore
		//	c.Status == "Linux" { // And if sriov-dplugin restarted
		//		if Z.demState("PLUGIN", d.NodeName) == true {
					// Make sure device plugin added resouce
		//			fmt.Printf("TODO: REstart DevPlugin Pod on the controller\n");
		//			return ctrl.Result{}, nil
		//		} // otherwise restart plugin
		//}
		//return ctrl.Result{}, nil
	}

	Z.idx = SnFinal
	return ctrl.Result{}, nil
	//return ctrl.Result{RequeueAfter: 60 /*nextRun.Sub(r.Now())*/}, nil
}

func (Z *StateControl) mkDevState(Command string) DevState {
	d := Z.dctx.Spec.InlineAcclrs[0] // Desired state from CRD
	devState := DevState{
		PciAddr:        d.PciAddr,
		NumVfs:         d.NumVfs,
		FwImage:        d.FwImage,
		FwTag:          d.FwTag,
		ResourcePrefix: d.ResourcePrefix,
		ResourceName:   d.ResourceName,
		Command:        Command,
	}
	return devState
}

func (Z *StateControl) s2AddDevice() (ctrl.Result, error) {

	fmt.Printf("-->     s2AddDevice()\n")

	// TODO: Use service

	d := Z.dctx.Spec.InlineAcclrs[0] // Desired state from CRD
	cPs := &corev1.PodList{}
	l := d.Acclr
	Z.rec.List(Z.ctx, cPs, client.MatchingLabels{"app": l + "-dev-control"},
		client.MatchingFields{"spec.nodeName": Z.dctx.Spec.NodeName},
	)
	if len(cPs.Items) == 0 {
		fmt.Printf("Config Pod Not found on: %s\n", Z.dctx.Spec.NodeName)
		Z.idx = SnFinal
		return ctrl.Result{}, nil
	}

	devState := Z.mkDevState("/BindDevice")
	devState, err := postAcclr(cPs.Items[0].Status.PodIP, devState)
	if err != nil {
		fmt.Printf(" Got: %+v\n\n and err of: %s", devState, err)
		Z.idx = SnFinal
		return ctrl.Result{RequeueAfter: 60}, nil
	}
	Z.idx = S3RestartDP
	return ctrl.Result{}, nil
}

func (Z *StateControl) s2RemoveDevice() (ctrl.Result, error) {

	fmt.Printf("-->     s2RemoveDevice()\n")

	d := Z.dctx.Spec.InlineAcclrs[0] // Desired state from CRD
	cPs := &corev1.PodList{}
	l := d.Acclr
	Z.rec.List(Z.ctx, cPs, client.MatchingLabels{"app": l + "-dev-control"},
		client.MatchingFields{"spec.nodeName": Z.dctx.Spec.NodeName},
	)
	if len(cPs.Items) == 0 {
		fmt.Printf("Config Pod Not found on: %s\n", Z.dctx.Spec.NodeName)
		Z.idx = SnFinal
		return ctrl.Result{}, nil
	}

	devState := Z.mkDevState("/UnbindDevice")
	devState, err := postAcclr(cPs.Items[0].Status.PodIP, devState)
	fmt.Printf(" Got: %+v\n\n and err of: %s", devState, err)
	// TODO:
	// Check for errors
	Z.idx = S3RestartDP
	return ctrl.Result{}, nil
}

func (Z *StateControl) s3RestartDP() (ctrl.Result, error) {

	fmt.Printf("-->     s3RestartDP()\n")

	Z.podStateRestart("sriovdp")

	Z.idx = SnFinal
	return ctrl.Result{}, nil
}

func (Z *StateControl) s2ReFlashDevice() (ctrl.Result, error) {

	fmt.Printf("-->     s2ReFlashDevice()\n")
	Z.idx = SnFinal

	p := corev1.Pod{}
	l := Z.dctx.Spec.InlineAcclrs[0].Acclr
	byf, err := ioutil.ReadFile("/manifests/dev-update/" + l + "-update.yaml")

	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return ctrl.Result{}, nil
	}

	reg, _ := regexp.Compile(`image: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("image: "+Z.dctx.Spec.InlineAcclrs[0].FwImage+":"+Z.dctx.Spec.InlineAcclrs[0].FwTag))
	reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("nodeName: "+Z.dctx.Spec.NodeName))
	reg, _ = regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte(Z.dctx.Spec.NodeName))
	reg, _ = regexp.Compile(`value: PCIADDR_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+Z.dctx.Spec.InlineAcclrs[0].PciAddr))

	err = yamlutil.Unmarshal(byf, &p)
	if err != nil {
		fmt.Printf("%s\n", err)
		return ctrl.Result{}, nil
	}
	err = ctrl.SetControllerReference(Z.dctx, &p, Z.rec.Scheme)
	if err != nil {
		fmt.Printf("Failed to set controller reference: %s\n", err)
		return ctrl.Result{}, err
	}

	err = Z.rec.Create(Z.ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create pod: %s\n", err)
		return ctrl.Result{}, nil
	}

	Z.idx = SnFinal
	return ctrl.Result{}, nil

}

func (Z *StateControl) snFinal() (ctrl.Result, error) {
	fmt.Printf("-->     snFinal()\n")
	Z.idx = SnFinal + 1 // we should never be here
	return ctrl.Result{}, nil
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
	dctx := &acclrv1beta1.OctNic{}
	if err := r.Get(ctx, req.NamespacedName, dctx); err != nil {
		fmt.Printf("unable to fetch OctNic Policy: %s\n", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	fmt.Printf("vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n")

	var Zi StateIf = &StateControl{}

	m, err := Zi.InitAndExecute(ctx, r, dctx)
	fmt.Printf("node: %s\n", dctx.Spec.NodeName)
	fmt.Printf("NumAcclr: %d err if any: %s\n", len(dctx.Spec.InlineAcclrs), err)
	fmt.Printf("^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\n")

	return m, err
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
