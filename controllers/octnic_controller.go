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

func update_pod_create(mctx *acclrv1beta1.OctNic, ctx context.Context,
	r *OctNicReconciler) error {

	/* Is there an instance of update tools on the node */
	dPod := &corev1.PodList{}
	r.List(ctx, dPod, client.MatchingLabels{"app": mctx.Spec.Acclr + "-dev-update"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName},
	)
	if len(dPod.Items) != 0 {
		return nil
	}

	nNodes := &corev1.NodeList{}
	r.Client.List(ctx, nNodes, []client.ListOption{
		client.MatchingLabels{NODE_LABEL_NAME: mctx.Spec.NodeName},
	}...)

	if len(nNodes.Items) != 0 {
		if nNodes.Items[0].Labels[MRVL_LABEL_KEY] != "true" {
			return nil
		}
	}
	fmt.Printf("Updating Node: %s\n", nNodes.Items[0].Name)

	// Label node that we are initializing the device
	nNodes.Items[0].Labels["marvell.com/inline_acclr_present"] = "initializing_fw"
	err := r.Update(ctx, &nNodes.Items[0])
	if err != nil {
		fmt.Printf("Failed to relabel %s: %s\n", nNodes.Items[0].Name, err)
		return err
	}

	// TODO: If device is managed by a driver, unbind it from the update tools
	// and bind it upon successfull update. This is needed for multi device support.

	p := corev1.Pod{}
	byf, err := ioutil.ReadFile("/manifests/dev-update/" + mctx.Spec.Acclr + "-update.yaml")
	if err != nil {
		fmt.Printf("Manifests not found: %s\n", err)
		return err
	}

	reg, _ := regexp.Compile(`image: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("image: "+mctx.Spec.FwImage+":"+mctx.Spec.FwTag))
	reg, _ = regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("nodeName: "+mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte(mctx.Spec.NodeName))
	reg, _ = regexp.Compile(`value: PCIADDR_FILLED_BY_OPERATOR`)
	byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.PciAddr))

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

	// Lable driver pod with inline_mrvl_acclr_driver_ready=true
	err = r.List(ctx, dPod,
		client.MatchingLabels{"inline_mrvl_acclr_driver_ready": "true"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName})
	if err != nil {
		fmt.Printf("Driver Pod does not exist: %s\n", err)
	}

	if len(dPod.Items) == 1 {
		fmt.Printf("Update Pod: update Driver Pod\n")
		dPod.Items[0].Labels["inline_mrvl_acclr_driver_ready"] = "false"
		err := r.Update(ctx, &dPod.Items[0])
		if err != nil {
			fmt.Printf("Failed to update driver pod: %s\n", err)
			return err
		}
	}

	/* If validate pod is running, delete it. */
	r.List(ctx, dPod,
		client.MatchingLabels{"app": mctx.Spec.Acclr + "-drv-validate"},
		client.MatchingFields{"spec.nodeName": mctx.Spec.NodeName})

	if len(dPod.Items) == 1 {
		fmt.Printf("Update Pod: delete Validation Pod\n")
		err = r.Delete(ctx, &dPod.Items[0])
		if err != nil {
			return err
		}
	}

	err = r.Create(ctx, &p)
	if err != nil {
		fmt.Printf("Failed to create update pod: %s\n", err)
		return err
	}

	return nil
}

func update_pod_check(mctx *acclrv1beta1.OctNic, ctx context.Context, r *OctNicReconciler) error {
	// Check device update pods
	updPods := &corev1.PodList{}
	r.List(ctx, updPods, client.MatchingLabels{"app": mctx.Spec.Acclr + "-dev-update"})
	for _, s := range updPods.Items {
		if s.Status.Phase == "Succeeded" {

			nNodes := &corev1.NodeList{}
			node := corev1.Node{}
			r.Client.List(ctx, nNodes, []client.ListOption{
				client.MatchingLabels{MRVL_LABEL_KEY: "initializing_fw"},
				client.MatchingLabels{NODE_LABEL_NAME: mctx.Spec.NodeName},
			}...)
			for _, x := range nNodes.Items {
				if x.Name == s.Spec.NodeName {
					node = x
					break
				}
			}
			if node.Name != s.Spec.NodeName {
				fmt.Printf("Update Failed on: %s %s, %s\n", node.Name, s.Spec.NodeName, mctx.Spec.PciAddr)
				return nil
			}

			node.Labels["marvell.com/inline_acclr_present"] = "fw_initialized"
			err := r.Update(ctx, &node)
			if err != nil {
				fmt.Printf("Failed to relabel %s: %s\n", node.Name, err)
				return err
			}

			err = r.Delete(ctx, &s)
			if err != nil {
				fmt.Printf("Failed to delete pod %s: %s\n", s.Name, err)
				return err
			}
		} else if s.Status.Phase == "Failed" {
			err := r.Delete(ctx, &s)
			if err != nil {
				fmt.Printf("Failed to delete pod %s: %s\n", s.Name, err)
				return err
			}
		}
	}
	return nil
}

func setup_validate_pod_check(mctx *acclrv1beta1.OctNic, ctx context.Context, r *OctNicReconciler) error {

	valPods := &corev1.PodList{}
	match_labels := mctx.Spec.Acclr + "-drv-validate"
	r.List(ctx, valPods, client.MatchingLabels{"app": match_labels})
	for _, s := range valPods.Items {
		dPod := &corev1.PodList{}
		if s.Status.Phase == "Succeeded" {
			r.List(ctx, dPod,
				client.MatchingLabels{"inline_mrvl_acclr_driver_ready": "false"},
				client.MatchingFields{"spec.nodeName": s.Spec.NodeName})
			if len(dPod.Items) == 1 {
				dPod.Items[0].Labels["inline_mrvl_acclr_driver_ready"] = "true"
				err := r.Update(ctx, &dPod.Items[0])
				if err != nil {
					fmt.Printf("Failed to relabel %s: %s\n", s.Name, err)
					return err
				}
			}
			/* Update OctNicDevice and OctNicStatus */
		} else if s.Status.Phase == "Failed" {
			// Delete driver pod
			r.List(ctx, dPod, client.MatchingLabels{"app": mctx.Spec.Acclr + "-driver"},
				client.MatchingFields{"spec.nodeName": s.Spec.NodeName})
			err := r.Delete(ctx, &dPod.Items[0])
			if err != nil {
				fmt.Printf("Failed to delete pod: %s\n", err)
				return err
			}
			// Delete the device plugin pod
			r.List(ctx, dPod,
				client.MatchingLabels{"app": mctx.Spec.Acclr + "-sriovdp"},
				client.MatchingFields{"spec.nodeName": s.Spec.NodeName})
			if len(dPod.Items) == 1 {
				err := r.Delete(ctx, &dPod.Items[0])
				if err != nil {
					fmt.Printf("Failed to delete pod: %s\n", err)
				}
			}
			// TODO:
			// Delete the validation pod
			// Delete driver pod on the node
			// Mark device on the node as failed after a 2 attempts?
			err = r.Delete(ctx, &s)
			if err != nil {
				fmt.Printf("Failed to delete pod: %s\n", err)
				return err
			}
		}
	}
	return nil
}

func setup_validate_pod_create(mctx *acclrv1beta1.OctNic, ctx context.Context, r *OctNicReconciler) error {
	var err error
	var match_labels string

	driverPods := &corev1.PodList{}
	err = r.List(ctx, driverPods,
		client.MatchingLabels{"inline_mrvl_acclr_driver_ready": "false"})
	if err != nil {
		fmt.Printf("Error reading Podlist: %s\n", err)
		return err
	}

	match_labels = mctx.Spec.Acclr + "-drv-validate"
	podManifest := "/manifests/drv-daemon-validate/" + mctx.Spec.Acclr + "-drv-validate.yaml"

	for _, s := range driverPods.Items {

		if s.Status.Phase == "Running" {
			updatePods := &corev1.PodList{}
			err = r.List(ctx, updatePods,
				client.MatchingLabels{"app": mctx.Spec.Acclr + "dev-update"},
				client.MatchingFields{"spec.nodeName": s.Spec.NodeName})

			if len(updatePods.Items) != 0 {
				continue
			}

			nNodes := &corev1.NodeList{}
			r.Client.List(ctx, nNodes, []client.ListOption{
				client.MatchingLabels{"kubernetes.io/hostname": mctx.Spec.NodeName},
			}...)

			if len(nNodes.Items) != 0 {
				if nNodes.Items[0].Labels[MRVL_LABEL_KEY] == "initializing_fw" {
					continue
				}
			}

			valPods := &corev1.PodList{}
			r.List(ctx, valPods,
				client.MatchingLabels{"app": match_labels},
				client.MatchingFields{"spec.nodeName": s.Spec.NodeName})

			if len(valPods.Items) == 0 {
				fmt.Printf("Start validator Pod %d\n", len(valPods.Items))
				p := corev1.Pod{}
				byf, err := ioutil.ReadFile(podManifest)
				if err != nil {
					fmt.Printf("Manifests not found: %s\n", err)
					return err
				}
				reg, _ := regexp.Compile(`nodeName: FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte("nodeName: "+s.Spec.NodeName))
				reg, _ = regexp.Compile(`NAME_FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte(s.Spec.NodeName))
				/* Device Configuration */
				/*
					fmt.Printf("++++++++++++++++++++\n")
					fmt.Printf("numvfs: %s\n", mctx.Spec.NumVfs)
					fmt.Printf("pciAddr: %s\n", mctx.Spec.PciAddr)
					fmt.Printf("prefix: %s\n", mctx.Spec.ResourcePrefix)
					fmt.Printf("prefix: %s\n", strings.Join(mctx.Spec.ResourceName[:], ","))
					fmt.Printf("--------------------\n")
				*/
				reg, _ = regexp.Compile(`value: NUMVFS_FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.NumVfs))
				reg, _ = regexp.Compile(`value: PCIADDR_FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.PciAddr))
				reg, _ = regexp.Compile(`value: PREFIX_FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte("value: "+mctx.Spec.ResourcePrefix))
				reg, _ = regexp.Compile(`value: RESOURCENAMES_FILLED_BY_OPERATOR`)
				byf = reg.ReplaceAll(byf, []byte("value: "+
					strings.Join(mctx.Spec.ResourceName[:], ",")))

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
			}
		}
	}
	return nil
}

func daemonset_create(mctx *acclrv1beta1.OctNic, ctx context.Context, r *OctNicReconciler, d string) error {

	var daemonName string
	var daemonManifest string
	switch d {
	case "DRIVER_DAEMON":
		daemonName = mctx.Spec.Acclr + "-driver"
		daemonManifest = "/manifests/drv-daemon/" + mctx.Spec.Acclr + "-drv.yaml"
	case "PLUGIN_DAEMON":
		daemonName = mctx.Spec.Acclr + "-sriovdp"
		daemonManifest = "/manifests/dev-plugin/" + mctx.Spec.Acclr + "-sriovdp.yaml"
	case "MONITR_DAEMON":
		/* TODO: Implement Monitor */
		fmt.Printf("Monitor not implemented\n")
	}
	/* Start daemonSet */
	daemSet := &appsv1.DaemonSet{}
	err := r.Get(context.TODO(),
		types.NamespacedName{Name: daemonName, Namespace: mctx.Namespace},
		daemSet)

	if errors.IsNotFound(err) {
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

	fmt.Printf("--> Reconcile reached\n")
	mctx := &acclrv1beta1.OctNic{}
	if err := r.Get(ctx, req.NamespacedName, mctx); err != nil {
		fmt.Printf("unable to fetch UpdateJob: %s\n", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// TODO:
	// The remote utlitites included in the tools image must
	// query the device for the sofware versions and reflash
	// if device's fw version differs from the CRD requested
	// version. This is currently not supported by the remote
	// utlitities and, therefore, the update stage will query
	// the nodes's label.

	err := update_pod_create(mctx, ctx, r)
	if err != nil {
		fmt.Printf("Failed to create update PoD: %s\n", err)
		return ctrl.Result{}, nil
	}
	err = update_pod_check(mctx, ctx, r)
	if err != nil {
		fmt.Printf("Failed update PoD: %s\n", err)
		return ctrl.Result{}, nil
	}

	// Start driver daemonSet if not already started
	err = daemonset_create(mctx, ctx, r, DRIVER_DAEMON)
	if err != nil {
		fmt.Printf("Failed to create daemonset: %s\n", err)
		return ctrl.Result{}, nil
	}

	// Driver Validation and device setup
	err = setup_validate_pod_create(mctx, ctx, r)
	if err != nil {
		fmt.Printf("Failed to create setup_validate: %s\n", err)
		return ctrl.Result{}, nil
	}

	// Check Driver Validation Pods
	err = setup_validate_pod_check(mctx, ctx, r)
	if err != nil {
		fmt.Printf("Failed setup_validate PoD: %s\n", err)
		return ctrl.Result{}, nil
	}

	// Device plugin
	err = daemonset_create(mctx, ctx, r, PLUGIN_DAEMON)
	if err != nil {
		fmt.Printf("Failed to create daemonset: %s\n", err)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
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
