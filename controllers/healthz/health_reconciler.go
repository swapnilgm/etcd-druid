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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// reconcilerName is the name of the controller
	reconcilerName = "healthz_reconciler"
)

// Reconciler is health recnociler object for external service created on etcd
type Reconciler struct {
	client.Client
}

// NewHealthReconciler return the health recnociler object for external service created on etcd
func NewHealthReconciler(cli client.Client) reconcile.Reconciler {
	return &Reconciler{
		Client: cli,
	}
}

// Reconcile reconciles the etcd health.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.TODO()
	etcd := &druidv1alpha1.Etcd{}
	if err := r.Get(ctx, req.NamespacedName, etcd); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	logger.Infof("Reconciling etcd: %s/%s", etcd.GetNamespace(), etcd.GetName())
	if etcd.Status.ServiceName == nil {
		logger.Infof("missing external service. Skipping reconciliation.")
		return ctrl.Result{
			RequeueAfter: time.Second * 5,
		}, nil
	}

	nodes := etcd.GetCompletedEtcdNodes()
	healthy := true
	for i := int32(0); i < nodes; i++ {
		client := http.DefaultClient
		url := fmt.Sprintf("%s-%d.%s-client-internal.%s.svc:%d/health", etcd.Name, i, etcd.Name, etcd.Namespace, etcd.GetCompletedBackupServerPort())
		resp, err := client.Get(url)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		if resp.StatusCode != http.StatusOK {
			healthy = false
			break
		}
	}

	if healthy {
		svc := &corev1.Service{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: *etcd.Status.ServiceName, Namespace: etcd.Namespace}, svc); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	return ctrl.Result{
		RequeueAfter: time.Second * 5,
	}, nil
}
