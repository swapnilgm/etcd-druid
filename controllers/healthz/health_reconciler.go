// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package healthz

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gardener/controller-manager-library/pkg/logger"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/v1alpha1"
	"github.com/gardener/etcd-druid/pkg/client/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// reconcilerName is the name of the controller
	reconcilerName = "healthz_reconciler"
	healthyLabel   = "healthy"
)

// Reconciler is health recocniler object for external service created on etcd
type Reconciler struct {
	clietnset kubernetes.Interface
	client    client.Client
}

// NewHealthReconciler return the health reconciler object for external service created on etcd
func NewHealthReconciler(cli kubernetes.Interface) reconcile.Reconciler {
	return &Reconciler{
		clietnset: cli,
		client:    cli.Client(),
	}
}

// Reconcile reconciles the etcd health.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.TODO()

	etcd := &druidv1alpha1.Etcd{}
	if err := r.client.Get(ctx, req.NamespacedName, etcd); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	logger.WithLogger(ctx, "reconciler", reconcilerName)
	logger.Infof("Reconciling etcd for health: %s/%s", etcd.GetNamespace(), etcd.GetName())
	if etcd.Status.ServiceName == nil {
		logger.Infof("missing external service. Skipping reconciliation.")
		return ctrl.Result{
			RequeueAfter: time.Second * 5,
		}, nil
	}

	healthy, err := r.getBackupHealthOverExec(ctx, etcd)
	if err != nil {
		return ctrl.Result{}, err
	}

	svc := &corev1.Service{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: *etcd.Status.ServiceName, Namespace: etcd.Namespace}, svc); err != nil {
		return ctrl.Result{}, err
	}

	if healthy {
		svcCopy := svc.DeepCopy()
		if svcCopy.Spec.Selector != nil {
			if _, ok := svcCopy.Spec.Selector[healthyLabel]; ok {
				// if present then only do the patch
				delete(svcCopy.Spec.Selector, healthyLabel)

				if err := r.client.Patch(ctx, svcCopy, client.MergeFrom(svc)); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	} else {
		svcCopy := svc.DeepCopy()
		if svcCopy.Spec.Selector != nil {
			if _, ok := svcCopy.Spec.Selector[healthyLabel]; !ok {
				// if not present then only add label and patch.
				svcCopy.Spec.Selector[healthyLabel] = "false"

				if err := r.client.Patch(ctx, svcCopy, client.MergeFrom(svc)); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

	return ctrl.Result{
		RequeueAfter: time.Second * 5,
	}, nil
}

func (r *Reconciler) getBackupHealthOverHTTP(ctx context.Context, etcd *druidv1alpha1.Etcd) (bool, error) {
	nodes := etcd.GetCompletedEtcdNodes()
	for i := int32(0); i < nodes; i++ {
		client := http.DefaultClient
		url := fmt.Sprintf("http://%s-%d.%s-internal.%s.svc.cluster.local:%d/healthz", etcd.Name, i, etcd.Name, etcd.Namespace, etcd.GetCompletedBackupServerPort())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode != http.StatusOK {
			return false, nil
		}
	}
	return true, nil
}

func (r *Reconciler) getBackupHealthOverExec(ctx context.Context, etcd *druidv1alpha1.Etcd) (bool, error) {
	pe := kubernetes.NewPodExecutor(r.clietnset)
	nodes := etcd.GetCompletedEtcdNodes()
	for i := int32(0); i < nodes; i++ {
		out, err := pe.Execute(ctx, etcd.Namespace, fmt.Sprint("%s-%d", etcd.Name, i), "backup-restore", "wget http://localhost:8080/healthz -S -O '-'")
		if err != nil {
			return false, err
		}
		decoder := yaml.NewYAMLOrJSONDecoder(out, 1024)
		bkpHealth := &health{}
		if err := decoder.Decode(bkpHealth); err != nil || !bkpHealth.health {
			return false, err
		}
	}
	return true, nil
}

type health struct {
	health bool `json:"health,omitempty"`
}
