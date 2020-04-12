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

	"github.com/gardener/controller-manager-library/pkg/logger"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// reconcilerName is the name of the controller
	reconcilerName = "healthz_reconciler"
)

type Reconciler struct {
	client.Client
}

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
	return ctrl.Result{}, nil
}
