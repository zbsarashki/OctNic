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
	//	"regexp"
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
var MONITR_DAEMON = "MONITR_DAEMON"
var PLUGIN_DAEMON = "PLUGIN_DAEMON"

var MRVL_LABEL_KEY = "marvell.com/inline_acclr_present"
var NODE_LABEL_NAME = "kubernetes.io/hostname"

type StateFunction func(context.Context, *OctNicReconciler, *acclrv1beta1.OctNic) (ctrl.Result, error)
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
}

type AcclrNodeState struct {
	drvState  bool
	dpState   bool
	monState  bool
	confState bool

	updState bool
	nodeName string
	axSet    []AxSet
}

type AxSet struct {
	activeResource bool
	ax             acclrv1beta1.InlineAcclr
}

func (Z *StateControl) init() {
	if Z.controller != nil {
		return
	}

	fmt.Printf("-->     init()\n")
	Z.controller = make([]StateFunction, SnFinal+1, SnFinal+1)
	Z.controller[S0CurNodeState] = Z.s0CurNodeState
	Z.controller[S0DaemsStart] = Z.s0DaemsStart
	Z.controller[S1SetNextState] = Z.s1SetNextState
	Z.controller[S2AddDevice] = Z.s2AddDevice
	Z.controller[S2RemoveDevice] = Z.s2RemoveDevice
	Z.controller[S3RestartDP] = Z.s3RestartDP
	Z.controller[SnFinal] = nil
}

func (Z *StateControl) InitAndExecute(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     InitAndExecute()\n")
	Z.init()
	rvl := 1 //  S0CurNodeState

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

	daemonName = dctx.Spec.InlineAcclrs[0].Acclr + "-monitor"
	err = r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: dctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.c.monState = false
		rvl = S0DaemsStart
	} else {
		Z.c.monState = true
	}

	daemonName = dctx.Spec.InlineAcclrs[0].Acclr + "-conf"
	err = r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: dctx.Namespace},
		&daemSet)

	if errors.IsNotFound(err) {
		Z.c.confState = false
		rvl = S0DaemsStart
	} else {
		Z.c.confState = true
	}

	Z.idx = rvl // S0DaemsStart OR S0CurNodeState

	fmt.Printf("-->     InitAndExecute() %d\n", Z.idx)
	// Loop
	var m ctrl.Result
	for fp := Z.controller[Z.idx]; Z.idx < SnFinal && Z.idx >= 0; fp = Z.controller[Z.idx] {

		fmt.Printf("-->     InitAndExecute() %d\n", Z.idx)
		m, err = fp(ctx, r, dctx)
		if err != nil {
			fmt.Printf("loop: %s\n", err)
			return m, nil
		}
	}
	return m, nil
}

func (Z *StateControl) s0DaemsStart(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s0DaemsStart()\n")
	// TODO:

	// Start driver dameonset

	if Z.c.drvState == false {
		daemonManifest := "/manifests/drv-daemon/" + dctx.Spec.InlineAcclrs[0].Acclr + "-drv.yaml"
		p := appsv1.DaemonSet{}
		byf, err := ioutil.ReadFile(daemonManifest)
		if err != nil {
			fmt.Printf("Manifests not found: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		yamlutil.Unmarshal(byf, &p)
		err = ctrl.SetControllerReference(dctx, &p, r.Scheme)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, &p)
		if err != nil {
			fmt.Printf("Failed to create pod: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
	}

	// Start Device plugin set

	if Z.c.dpState == false {
		daemonManifest := "/manifests/dev-plugin/" + dctx.Spec.InlineAcclrs[0].Acclr + "-sriovdp.yaml"

		p := appsv1.DaemonSet{}
		byf, err := ioutil.ReadFile(daemonManifest)
		if err != nil {
			fmt.Printf("Manifests not found: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		yamlutil.Unmarshal(byf, &p)
		err = ctrl.SetControllerReference(dctx, &p, r.Scheme)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, &p)
		if err != nil {
			fmt.Printf("Failed to create pod: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
	}

	// Start Monitor set

	if Z.c.monState == false {
		daemonManifest := "/manifests/dev-monitor/" + dctx.Spec.InlineAcclrs[0].Acclr + "-monitor.yaml"

		p := appsv1.DaemonSet{}
		byf, err := ioutil.ReadFile(daemonManifest)
		if err != nil {
			fmt.Printf("Manifests not found: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		yamlutil.Unmarshal(byf, &p)
		err = ctrl.SetControllerReference(dctx, &p, r.Scheme)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, &p)
		if err != nil {
			fmt.Printf("Failed to create pod: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
	}

	// Start Config set

	if Z.c.confState == false {
		daemonManifest := "/manifests/dev-conf/" + dctx.Spec.InlineAcclrs[0].Acclr + "-conf.yaml"

		p := appsv1.DaemonSet{}
		byf, err := ioutil.ReadFile(daemonManifest)
		if err != nil {
			fmt.Printf("Manifests not found: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		yamlutil.Unmarshal(byf, &p)
		err = ctrl.SetControllerReference(dctx, &p, r.Scheme)
		if err != nil {
			fmt.Printf("Failed to set controller reference: %s\n", err)
			Z.idx = SnFinal
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, &p)
		if err != nil {
			fmt.Printf("Failed to create pod: %s\n", err)
			return ctrl.Result{}, err
		}
	}

	Z.idx = S0CurNodeState
	return ctrl.Result{}, nil
}

func (Z *StateControl) s0CurNodeState(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s0CurNodeState()\n")

	mPs := &corev1.PodList{}
	l := dctx.Spec.InlineAcclrs[0].Acclr
	r.List(ctx, mPs, client.MatchingLabels{"app": l + "-monitor"},
		client.MatchingFields{"spec.nodeName": dctx.Spec.NodeName},
	)
	if len(mPs.Items) == 0 {
		fmt.Printf("Montior Pod Not found on: %s\n", dctx.Spec.NodeName)
		Z.idx = SnFinal
		return ctrl.Result{Requeue: true, RequeueAfter: 60}, nil
	}

	// TODO: This part needs to change
	e := dctx.Spec.InlineAcclrs[0]
	devstate, err := getAcclrState(mPs.Items[0].Status.PodIP, e.PciAddr)
	if err != nil {
		fmt.Printf("Failed getAcclrState : %s\n", err)
		Z.idx = SnFinal
		return ctrl.Result{Requeue: true, RequeueAfter: 60}, nil
	}

	rvl := acclrv1beta1.InlineAcclr{}
	rvl.PciAddr = devstate.PciAddr
	rvl.NumVfs = devstate.NumVfs
	rvl.FwImage = devstate.FwImage
	rvl.FwTag = devstate.FwTag

	// TODO: we need to query dP or k8
	a := false
	if devstate.PfDriver != "" {
		a = true
	}

	Z.c.axSet = append(Z.c.axSet, AxSet{activeResource: a, ax: rvl})

	Z.idx = S1SetNextState
	return ctrl.Result{}, nil
}

func (Z *StateControl) s1SetNextState(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s1SetNextState()\n")
	c := Z.c.axSet[0]
	d := dctx.Spec.InlineAcclrs[0]

	/*
		mPs := &corev1.PodList{}
		l := dctx.Spec.InlineAcclrs[0].Acclr
		r.List(ctx, mPs, client.MatchingLabels{"app": l + "-monitor"},
			client.MatchingFields{"spec.nodeName": dctx.Spec.NodeName},
		)
		if len(mPs.Items) == 0 {
			fmt.Printf("Montior Pod Not found on: %s\n", dctx.Spec.NodeName)
			Z.idx = SnFinal
			return ctrl.Result{Requeue: true, RequeueAfter: 60}, nil
		}
	*/

	if d.PciAddr == c.ax.PciAddr {
		if (d.FwImage != c.ax.FwImage) || (d.FwTag != c.ax.FwTag) {
			if c.activeResource == true {
				Z.idx = S2RemoveDevice
				return ctrl.Result{}, nil
			}
			Z.idx = S2ReFlashDevice
			return ctrl.Result{}, nil
		}

		if c.activeResource == true {
			Z.idx = SnFinal
			return ctrl.Result{}, nil
		}
		Z.idx = S2AddDevice
		return ctrl.Result{}, nil
	}
	Z.idx = SnFinal
	return ctrl.Result{}, nil
}

func (Z *StateControl) s2AddDevice(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s2AddDevice()\n")

	Z.idx = SnFinal
	return ctrl.Result{}, nil
}

func (Z *StateControl) s2RemoveDevice(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s2RemoveDevice()\n")

	c := Z.c.axSet[0]              // Current State from s0CurNodeState()
	d := dctx.Spec.InlineAcclrs[0] // Desired state from CRD

	fmt.Printf("device as per dctx:\n")
	fmt.Printf("%v\n", d)
	fmt.Printf("device as per axSet:\n")
	fmt.Printf("%v\n", c)

	cPs := &corev1.PodList{}
	l := d.Acclr
	r.List(ctx, cPs, client.MatchingLabels{"app": l + "-conf"},
		client.MatchingFields{"spec.nodeName": dctx.Spec.NodeName},
	)
	if len(cPs.Items) == 0 {
		fmt.Printf("Config Pod Not found on: %s\n", dctx.Spec.NodeName)
		Z.idx = SnFinal
		return ctrl.Result{Requeue: true, RequeueAfter: 60}, nil
	}

	devstate, err := postAcclrUnbind(cPs.Items[0].Status.PodIP, d.PciAddr)
	fmt.Printf(" Got: %+v\n\n and err of: %s", devstate, err)
	Z.idx = S3RestartDP
	return ctrl.Result{}, nil
}

func (Z *StateControl) s3RestartDP(
	ctx context.Context,
	r *OctNicReconciler,
	dctx *acclrv1beta1.OctNic) (ctrl.Result, error) {

	fmt.Printf("-->     s3RestartDP()\n")
	Z.idx = SnFinal
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
