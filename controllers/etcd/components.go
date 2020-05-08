// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package etcd

import (
	"context"
	"fmt"
	"strings"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/v1alpha1"
	"github.com/gardener/etcd-druid/pkg/common"
	"github.com/gardener/etcd-druid/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/imagevector"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *Reconciler) getInternalServiceFromEtcd(etcd *druidv1alpha1.Etcd) (*corev1.Service, error) {
	sslPrefix := ""
	if etcd.Spec.Etcd.TLS != nil {
		sslPrefix = "-ssl"
	}

	selector, err := metav1.LabelSelectorAsMap(etcd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-client-internal", etcd.Name),
			Namespace: etcd.Namespace,
			Labels:    utils.MergeStringMaps(selector, map[string]string{"scope": "internal"}),
			Annotations: map[string]string{
				"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Type:                     corev1.ServiceTypeClusterIP,
			Selector:                 selector,
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       fmt.Sprintf("etcd-server%s", sslPrefix),
					Protocol:   corev1.ProtocolTCP,
					Port:       2380,
					TargetPort: intstr.FromInt(2380),
				},
				{
					Name:       fmt.Sprintf("etcd-client%s", sslPrefix),
					Protocol:   corev1.ProtocolTCP,
					Port:       2379,
					TargetPort: intstr.FromInt(2379),
				},
				{
					Name:       "backup",
					Protocol:   corev1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(etcd, svc, r.Scheme); err != nil {
		return nil, err
	}

	return svc, nil
}

func (r *Reconciler) getExternalServiceFromEtcd(etcd *druidv1alpha1.Etcd) (*corev1.Service, error) {
	sslPrefix := ""
	if etcd.Spec.Etcd.TLS != nil {
		sslPrefix = "-ssl"
	}

	selector, err := metav1.LabelSelectorAsMap(etcd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-client-external", etcd.Name),
			Namespace: etcd.Namespace,
			Labels: utils.MergeStringMaps(selector, map[string]string{
				"scope":   "external",
				"healthy": "false",
			}),
		},
		Spec: corev1.ServiceSpec{
			//ClusterIP:                assign existing clusterip
			Type:                     corev1.ServiceTypeClusterIP,
			Selector:                 utils.MergeStringMaps(selector, map[string]string{"healthy": "false"}),
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       fmt.Sprintf("client%s", sslPrefix),
					Protocol:   corev1.ProtocolTCP,
					Port:       2379,
					TargetPort: intstr.FromInt(2379),
				},
				{
					Name:       "backup",
					Protocol:   corev1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(etcd, svc, r.Scheme); err != nil {
		return nil, err
	}

	return svc, nil
}

func (r *Reconciler) getConfigMapFromEtcd(etcd *druidv1alpha1.Etcd, svc *corev1.Service) (*corev1.ConfigMap, error) {

	selector, err := metav1.LabelSelectorAsMap(etcd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-bootstrap", etcd.Name),
			Namespace: etcd.Namespace,
			Labels:    selector,
		},
		Data: map[string]string{
			"bootstrap.sh": r.getBootstrapScript(etcd, svc),
			//"etcd.conf.yaml":   r.getEtcdConf(etcd, svc),
			"etcdbr.conf.yaml": r.getEtcdBRConf(etcd, svc),
		},
	}

	if err := controllerutil.SetControllerReference(etcd, cm, r.Scheme); err != nil {
		return nil, err
	}
	return cm, nil
}

func (r *Reconciler) getBootstrapScript(etcd *druidv1alpha1.Etcd, svc *corev1.Service) string {
	sb := strings.Builder{}
	// Script header
	sb.WriteString(`#!/bin/sh
VALIDATION_MARKER=/var/etcd/data/validation_marker
`)
	appendS := ""
	if etcd.Spec.Etcd.TLS != nil {
		appendS = "s"
		// install wget dependency
		sb.WriteString(`
# install wget from apk in order to pass --ca-certificate flag because
# busybox wget only has bare minimum features, without --ca-certificate option
apk update
if [ $? -ne 0 ]; then
    echo "apk update failed"
    exit 1
  fi
apk add wget
if [ $? -ne 0 ]; then
  echo "failed to update wget"
  exit 1
fi
# ensure that newly updated wget comes with --ca-certificate flag
wget --help | grep -- --ca-certificate
if [ $? -ne 0 ]; then
  echo "updated wget is missing --ca-certificate flag"
  exit 1
fi`)
	}

	sb.WriteString(`
trap_and_propagate() {
    PID=$1
    shift
    for sig in "$@" ; do
        trap "kill -$sig $PID" "$sig"
    done
}
`)
	sb.WriteString(`
start_managed_etcd() {
    rm -rf $VALIDATION_MARKER
	etcd --name=$MY_POD_NAME \
`)

	// Path to the data directory.
	sb.WriteString("    --data-dir=/var/etcd/data/new.etcd \\\n")
	//metrics configuration
	sb.WriteString(fmt.Sprintf("    --metrics=%s \\\n", etcd.Spec.Etcd.Metrics))

	//Number of committed transactions to trigger a snapshot to disk.
	sb.WriteString("    --snapshot-count=75000 \\\n")

	// Accept etcd V2 client requests
	sb.WriteString("    --enable-v2=false \\\n")

	// Raise alarms when backend size exceeds the given quota. 0 means use the default quota.
	if etcd.Spec.Etcd.Quota != nil {
		sb.WriteString(fmt.Sprintf("    --quota-backend-bytes=%d \\\n", etcd.Spec.Etcd.Quota.Value()))
	}

	// // List of comma separated URLs to listen on for peer traffic.
	// appendS := ""
	// if etcd.Spec.Etcd.TLS != nil {
	// 	appendS = "s"
	// 	sb.WriteString(`
	// client-transport-security:
	//   # Path to the client server TLS cert file.
	//   cert-file: /var/etcd/ssl/server/tls.crt

	//   # Path to the client server TLS key file.
	//   key-file: /var/etcd/ssl/server/tls.key

	//   # Enable client cert authentication.
	//   client-cert-auth: true

	//   # Path to the client server TLS trusted CA cert file.
	//   trusted-ca-file: /var/etcd/ssl/ca/ca.crt

	//   # Client TLS using generated certificates
	//   auto-tls: false
	//   `)
	// }
	sb.WriteString(fmt.Sprintf("    --listen-peer-urls=http%s://0.0.0.0:%d \\\n", appendS, etcd.GetCompletedEtcdServerPort()))

	// List of comma separated URLs to listen on for client traffic.
	sb.WriteString(fmt.Sprintf("    --listen-client-urls=http%s://0.0.0.0:%d \\\n", appendS, etcd.GetCompletedEtcdClientPort()))

	// List of this member's peer URLs to advertise to the rest of the cluster.
	// The URLs needed to be a comma-separated list.
	sb.WriteString(fmt.Sprintf("    --initial-advertise-peer-urls=\"http%s://$MY_POD_IP:%d\" \\\n", appendS, etcd.GetCompletedEtcdServerPort()))

	// List of this member's client URLs to advertise to the public.
	// The URLs needed to be a comma-separated list.
	sb.WriteString(fmt.Sprintf("    --advertise-client-urls=\"http%s://$MY_POD_IP:%d\" \\\n", appendS, etcd.GetCompletedEtcdClientPort()))

	nodes := etcd.GetCompletedEtcdNodes()
	initialCluster := ""
	for i := int32(0); i < nodes; i++ {
		initialCluster = fmt.Sprintf("%s,%s-%d=http://%s-%d.%s:%d", initialCluster, etcd.Name, i, etcd.Name, i, svc.Name, etcd.GetCompletedEtcdServerPort())
	}
	// DNS domain used to bootstrap initial cluster.
	//sb.WriteString(fmt.Sprintf("    --discovery-srv=%s \\\n", svc.Name))
	// Initial cluster configuration for bootstrapping.
	sb.WriteString(fmt.Sprintf("    --initial-cluster=%s \\\n", initialCluster))
	// List of this member's client URLs to advertise to the public.
	// The URLs needed to be a comma-separated list.
	// Initial cluster token for the etcd cluster during bootstrap.
	sb.WriteString(fmt.Sprintf("    --initial-cluster-token=%s \\\n", etcd.Name))
	// Initial cluster state ('new' or 'existing').
	sb.WriteString("    --initial-cluster-state=new \\\n")
	sb.WriteString("    --auto-compaction-mode=periodic \\\n")
	sb.WriteString("    --auto-compaction-retention=\"24\"\n")
	sb.WriteString(`
    ETCDPID=$!
    trap_and_propagate $ETCDPID INT TERM
    wait $ETCDPID
    RET=$?
    echo $RET > $VALIDATION_MARKER
    exit $RET
}
`)

	sb.WriteString(`
check_and_start_etcd() {
    while true;
    do
        wget `)

	if etcd.Spec.Etcd.TLS != nil {
		sb.WriteString(`--ca-certificate=/var/etcd/ssl/ca/ca.crt "https`)
	} else {
		sb.WriteString(`"http`)
	}
	sb.WriteString(fmt.Sprintf("://$MY_POD_NAME:%d/initialization/status\" -S -O status;\n", etcd.GetCompletedBackupServerPort()))
	sb.WriteString("        STATUS=`cat status`;\n")
	sb.WriteString(`        case $STATUS in
          "New")
            wget `)
	if etcd.Spec.Etcd.TLS != nil {
		sb.WriteString(`--ca-certificate=/var/etcd/ssl/ca/ca.crt "https`)
	} else {
		sb.WriteString(`"http`)
	}
	sb.WriteString(fmt.Sprintf("://$MY_POD_NAME:%d/initialization/start?mode=$1\" -S -O - ;;", etcd.GetCompletedBackupServerPort()))
	sb.WriteString(`
          "Progress")
            sleep 1;
            continue;;
          "Failed")
            continue;;
    	  "Successful")
            echo "Bootstrap preprocessing end time: $(date)"
            start_managed_etcd
            break;;
        esac;
    done
}
`)

	sb.WriteString(`
echo "Bootstrap preprocessing start time: $(date)"
if [ ! -f $VALIDATION_MARKER ] ;then
    echo "No $VALIDATION_MARKER file. Perform complete initialization routine and start etcd."
    check_and_start_etcd full
else
    echo "$VALIDATION_MARKER file present. Check return status and decide on initialization"
    run_status=`)
	sb.WriteString("`cat $VALIDATION_MARKER`")
	sb.WriteString(`
    echo "$VALIDATION_MARKER content: $run_status"
    if [ $run_status == '143' ] || [ $run_status == '130' ] || [ $run_status == '0' ] ; then
        echo "Requesting sidecar to perform sanity validation"
        check_and_start_etcd sanity
    else
        echo "Requesting sidecar to perform full validation"
        check_and_start_etcd full
    fi
fi`)
	return sb.String()
}

func (r *Reconciler) getEtcdConf(etcd *druidv1alpha1.Etcd, svc *corev1.Service) string {
	// TODO: Should we make use of etcd.Config struct and then yaml encoder.
	sb := strings.Builder{}

	// Path to the data directory.
	sb.WriteString("data-dir: /var/etcd/data/new.etcd\n")

	//metrics configuration
	sb.WriteString(fmt.Sprintf("metrics: %s\n", etcd.Spec.Etcd.Metrics))

	//Number of committed transactions to trigger a snapshot to disk.
	sb.WriteString("snapshot-count: 75000\n")

	// Accept etcd V2 client requests
	sb.WriteString("enable-v2: false\n")

	// Raise alarms when backend size exceeds the given quota. 0 means use the default quota.
	if etcd.Spec.Etcd.Quota != nil {
		sb.WriteString(fmt.Sprintf("quota-backend-bytes: %d\n", etcd.Spec.Etcd.Quota.Value()))
	}

	// List of comma separated URLs to listen on for peer traffic.
	appendS := ""
	if etcd.Spec.Etcd.TLS != nil {
		appendS = "s"
		sb.WriteString(`
    client-transport-security:
      # Path to the client server TLS cert file.
      cert-file: /var/etcd/ssl/server/tls.crt

      # Path to the client server TLS key file.
      key-file: /var/etcd/ssl/server/tls.key

      # Enable client cert authentication.
      client-cert-auth: true

      # Path to the client server TLS trusted CA cert file.
      trusted-ca-file: /var/etcd/ssl/ca/ca.crt

      # Client TLS using generated certificates
	  auto-tls: false
	  `)
	}
	sb.WriteString(fmt.Sprintf("listen-peer-urls: http%s://0.0.0.0:%d\n", appendS, etcd.GetCompletedEtcdServerPort()))

	// List of comma separated URLs to listen on for client traffic.
	sb.WriteString(fmt.Sprintf("listen-client-urls: http%s://0.0.0.0:%d\n", appendS, etcd.GetCompletedEtcdClientPort()))

	// List of this member's peer URLs to advertise to the rest of the cluster.
	// The URLs needed to be a comma-separated list.
	//sb.WriteString(fmt.Sprintf("initial-advertise-peer-urls: http%s://$MY_POD_IP:%d\n", appendS, getCompletedEtcdServerPort(etcd)))

	// List of this member's client URLs to advertise to the public.
	// The URLs needed to be a comma-separated list.
	//sb.WriteString(fmt.Sprintf("advertise-client-urls: http%s://$MY_POD_IP:%d\n", appendS, getCompletedEtcdClientPort(etcd)))

	// DNS domain used to bootstrap initial cluster.
	sb.WriteString(fmt.Sprintf("discovery-srv: %s\n", svc.Name))
	// Initial cluster token for the etcd cluster during bootstrap.
	sb.WriteString(fmt.Sprintf("initial-cluster-token: %s\n", etcd.Name))

	// Initial cluster state ('new' or 'existing').
	sb.WriteString("initial-cluster-state: new\n")

	sb.WriteString("auto-compaction-mode: periodic\n")
	sb.WriteString("auto-compaction-retention: \"24\"\n")

	return sb.String()
}

func (r *Reconciler) getEtcdBRConf(etcd *druidv1alpha1.Etcd, svc *corev1.Service) string {
	// TODO: Should we make use of etcd.Config struct and then yaml encoder.
	sb := strings.Builder{}
	sb.WriteString("etcdConnectionConfig:\n")
	appendS := ""
	if etcd.Spec.Etcd.TLS != nil {
		appendS = "s"
		// # username: admin
		//  # password: admin
		sb.WriteString(`

    insecureTransport: false
    insecureSkipVerify: false
    certFile: /var/etcd/ssl/client/tls.crt
    keyFile: /var/etcd/ssl/client/tls.key
    caFile: /var/etcd/ssl/client/ca.crt
		`)
	}
	sb.WriteString("  endpoints:\n")
	sb.WriteString(fmt.Sprintf("    - http%s://%s:%d\n", appendS, svc.Name, etcd.GetCompletedEtcdClientPort()))

	sb.WriteString("  connectionTimeout: 5m\n")
	sb.WriteString("\nserverConfig:\n")
	sb.WriteString(fmt.Sprintf("  port: %d\n", etcd.GetCompletedBackupServerPort()))
	if etcd.Spec.Backup.TLS != nil {
		sb.WriteString(`  server-cert: /var/etcd/ssl/server/tls.crt
    server-key: /var/etcd/ssl/server/tls.key`)
	}

	sb.WriteString("\nleaderElectionConfig:\n")
	sb.WriteString("  retryPeriod: 5s\n")

	sb.WriteString("\nsnapshotterConfig:\n")
	if etcd.Spec.Backup.FullSnapshotSchedule != nil {
		sb.WriteString(fmt.Sprintf("  schedule: %s\n", *etcd.Spec.Backup.FullSnapshotSchedule))
	}
	if etcd.Spec.Backup.DeltaSnapshotPeriod != nil {
		sb.WriteString(fmt.Sprintf("  deltaSnapshotPeriod: %s\n", etcd.Spec.Backup.DeltaSnapshotPeriod.Duration))
	}

	if etcd.Spec.Backup.DeltaSnapshotMemoryLimit != nil {
		sb.WriteString(fmt.Sprintf("  deltaSnapshotMemoryLimit: %d\n", etcd.Spec.Backup.DeltaSnapshotMemoryLimit.Value()))
	}

	if etcd.Spec.Backup.GarbageCollectionPeriod != nil {
		sb.WriteString(fmt.Sprintf("  garbageCollectionPeriod: %s\n", etcd.Spec.Backup.GarbageCollectionPeriod.Duration))
	}

	if etcd.Spec.Backup.GarbageCollectionPolicy != nil {
		sb.WriteString(fmt.Sprintf("  garbageCollectionPolicy: %s\n", *etcd.Spec.Backup.GarbageCollectionPolicy))
		// if etcd.Spec.Backup.GarbageCollectionPolicy == druidv1alpha1.GarbageCollectionPolicyLimitBased && etcd.Spec.Backup.MaxBackups != nil {
		// 	sb.WriteString(fmt.Sprintf("  maxBackups: %d", *etcd.Spec.Backup.MaxBackups))
		// }
	}
	if etcd.Spec.Backup.Store != nil {

		sb.WriteString("\nsnapstoreConfig:\n")

		sb.WriteString(fmt.Sprintf("  container: %s\n", *etcd.Spec.Backup.Store.Container))
		sb.WriteString(fmt.Sprintf("  provider: %s\n", *etcd.Spec.Backup.Store.Provider))

		sb.WriteString(fmt.Sprintf("  prefix: %s\n", etcd.Spec.Backup.Store.Prefix))
	}

	sb.WriteString("\nrestorationConfig:\n")
	sb.WriteString("  name: default\n")
	sb.WriteString(fmt.Sprintf("  initialCluster: \"default=http://localhost:%d\"\n", etcd.GetCompletedEtcdServerPort()))
	sb.WriteString(fmt.Sprintf("  initialClusterToken: %s\n", etcd.Name))
	sb.WriteString("  restoreDataDir: /var/etcd/data/new.etcd\n")
	sb.WriteString("  initialAdvertisePeerURLs:\n")
	sb.WriteString(fmt.Sprintf("    - \"http://localhost:%d\"\n", etcd.GetCompletedEtcdServerPort()))

	if etcd.Spec.Etcd.Quota != nil {
		sb.WriteString(fmt.Sprintf("  embeddedEtcdQuotaBytes: %d\n", etcd.Spec.Etcd.Quota.Value()))
	}

	if etcd.Spec.Etcd.DefragmentationSchedule != nil {
		sb.WriteString(fmt.Sprintf("defragmentationSchedule: %s\n", *etcd.Spec.Etcd.DefragmentationSchedule))
	}

	return sb.String()
}

func (r *Reconciler) getStatefulSetFromEtcd(ctx context.Context, etcd *druidv1alpha1.Etcd, cm *corev1.ConfigMap, svc *corev1.Service) (*appsv1.StatefulSet, error) {
	selector, err := metav1.LabelSelectorAsMap(etcd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	var images map[string]*imagevector.Image
	if etcd.Spec.Etcd.Image == nil || etcd.Spec.Backup.Image == nil {
		imageNames := []string{
			common.Etcd,
			common.BackupRestore,
		}
		images, err = imagevector.FindImages(r.ImageVector, imageNames)
		if err != nil {
			return nil, err
		}
	}

	var etcdImage, backupImage string
	if etcd.Spec.Etcd.Image == nil {
		val, ok := images[common.Etcd]
		if !ok {
			return nil, fmt.Errorf("either etcd resource or image vector should have %s image", common.Etcd)
		}
		etcdImage = val.String()
	} else {
		etcdImage = *etcd.Spec.Etcd.Image
	}

	if etcd.Spec.Backup.Image == nil {
		val, ok := images[common.BackupRestore]
		if !ok {
			return nil, fmt.Errorf("either etcd resource or image vector should have %s image", common.BackupRestore)
		}
		backupImage = val.String()
	} else {
		backupImage = *etcd.Spec.Backup.Image
	}

	var (
		bootstrapDefaultMode int32 = 356
		//etcdConfigDefaultMode   int32 = 0644
		etcdbrConfigDefaultMode int32 = 0644
	)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcd.Name,
			Namespace: etcd.Namespace,
			Labels:    selector,
			//TODO: Add Annotations
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: etcd.Spec.Replicas,
			Selector: etcd.Spec.Selector,

			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
					//TODO: Add Annotations
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "etcd-bootstrap-sh",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: cm.Name},
									DefaultMode:          &bootstrapDefaultMode,
									Items: []corev1.KeyToPath{
										{
											Key:  "bootstrap.sh",
											Path: "bootstrap.sh",
										},
									},
								},
							},
						},

						// {
						// 	Name: "etcd-config-file",
						// 	VolumeSource: corev1.VolumeSource{
						// 		ConfigMap: &corev1.ConfigMapVolumeSource{
						// 			LocalObjectReference: v1.LocalObjectReference{Name: cm.Name},
						// 			DefaultMode:          &etcdConfigDefaultMode,
						// 			Items: []corev1.KeyToPath{
						// 				{
						// 					Key:  "etcd.conf.yaml",
						// 					Path: "etcd.conf.yaml",
						// 				},
						// 			},
						// 		},
						// 	},
						// },
						{
							Name: "etcdbr-config-file",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: cm.Name},
									DefaultMode:          &etcdbrConfigDefaultMode,
									Items: []corev1.KeyToPath{
										{
											Key:  "etcdbr.conf.yaml",
											Path: "etcdbr.conf.yaml",
										},
									},
								},
							},
						},
						// {
						// 	Name: "etcd-server-tls",
						// 	VolumeSource: corev1.VolumeSource{
						// 		Secret: &corev1.SecretVolumeSource{
						// 			SecretName: "etcd-server-tls",
						// 			//DefaultMode:356,

						// 		},
						// 	},
						// },

						// {
						// 	Name: "etcd-client-tls",
						// 	VolumeSource: corev1.VolumeSource{
						// 		Secret: &corev1.SecretVolumeSource{
						// 			SecretName: "etcd-client-tls",
						// 			//DefaultMode:356,

						// 		},
						// 	},
						// },

						// {
						// 	Name: "ca-etcd",
						// 	VolumeSource: corev1.VolumeSource{
						// 		Secret: &corev1.SecretVolumeSource{
						// 			SecretName: "ca-etcds",
						// 			//DefaultMode:356,
						// 		},
						// 	},
						// },

						// {
						// 	Name: "etcd-backup",
						// 	VolumeSource: corev1.VolumeSource{
						// 		Secret: &corev1.SecretVolumeSource{
						// 			SecretName: "etcd-backu",
						// 			//DefaultMode:356,
						// 		},
						// 	},
						// },
					},

					Containers: []corev1.Container{
						{
							Name:  "etcd",
							Image: etcdImage,
							Command: []string{
								"/var/etcd/bin/bootstrap.sh",

								// "etcd",
								// //"--config-file /var/etcd/config/etcd.conf.yaml"
								// "--name=$(MY_POD_NAME)",
								// "--initial-advertise-peer-urls=http://$(MY_POD_IP):2380",
								// "--advertise-client-urls=http://$(MY_POD_IP):2379",
								// "--data-dir=/var/etcd/data/new.etcd",
								// "--listen-peer-urls=http://0.0.0.0:2380",
								// "--listen-client-urls=http://0.0.0.0:2379",
								// "--initial-cluster-token=HOST",
								// "--initial-cluster-state=existing",
								// fmt.Sprintf("--discovery-srv=%s", svc.Name),
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(2380),
									},
								},
								InitialDelaySeconds: 0,
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: int32(etcd.GetCompletedEtcdServerPort()),
									Name:          "etcd-server",
									Protocol:      corev1.ProtocolTCP,
								},
								{
									ContainerPort: int32(etcd.GetCompletedEtcdClientPort()),
									Name:          "etcd-client",
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      *etcd.Spec.VolumeClaimTemplate,
									MountPath: "/var/etcd/data/",
								},
								{
									Name:      "etcd-bootstrap-sh",
									MountPath: "/var/etcd/bin/",
								},
								// {
								// 	Name:      "etcd-config-file",
								// 	MountPath: "/var/etcd/config/",
								// },
								// {
								// 	Name:      "ca-etcd",
								// 	MountPath: "/var/etcd/ssl/ca",
								// },
								// {
								// 	Name:      "etcd-server-tls",
								// 	MountPath: "/var/etcd/ssl/server",
								// },
								// {
								// 	Name:      "etcd-client-tls",
								// 	MountPath: "/var/etcd/ssl/client",
								// },
							},
							Env: []corev1.EnvVar{
								{
									Name: "MY_POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name: "MY_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
						},
						{
							Name: "backup-restore",
							Command: []string{
								"etcdbrctl",
								"server",
								"--config-file=/var/etcdbr/config/etcdbr.conf.yaml",
							},
							Image: backupImage,

							Ports: []corev1.ContainerPort{
								{

									ContainerPort: int32(etcd.GetCompletedBackupServerPort()),
									Name:          "backup",
									Protocol:      corev1.ProtocolTCP,
								},
							},

							Env: []corev1.EnvVar{
								{
									Name:  "STORAGE_CONTAINER",
									Value: "backup", //*etcd.Spec.Backup.Store.Container,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      *etcd.Spec.VolumeClaimTemplate,
									MountPath: "/var/etcd/data/",
								},
								{
									Name:      "etcd-bootstrap-sh",
									MountPath: "/var/etcd/bin/",
								},
								{
									Name:      "etcdbr-config-file",
									MountPath: "/var/etcdbr/config/",
								},
								// {
								// 	Name:      "ca-etcd",
								// 	MountPath: "/var/etcd/ssl/ca",
								// },
								// {
								// 	Name:      "etcd-server-tls",
								// 	MountPath: "/var/etcd/ssl/server",
								// },
								// {
								// 	Name:      "etcd-client-tls",
								// 	MountPath: "/var/etcd/ssl/client",
								// },
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: *etcd.Spec.VolumeClaimTemplate,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: etcd.Spec.StorageClass,

						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: *etcd.Spec.StorageCapacity,
							},
						},
					},
				},
			},

			ServiceName:         svc.Name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
		},
	}

	if etcd.Spec.PriorityClassName != nil {
		ss.Spec.Template.Spec.PriorityClassName = *etcd.Spec.PriorityClassName
	}

	if err := controllerutil.SetControllerReference(etcd, ss, r.Scheme); err != nil {
		return nil, err
	}

	return ss, nil
}

// func (r *Reconciler) getStatefulSetFromEtcd(etcd *druidv1alpha1.Etcd, cm *corev1.ConfigMap, svc *corev1.Service, values map[string]interface{}) (*appsv1.StatefulSet, error) {
// 	var err error
// 	decoded := &appsv1.StatefulSet{}
// 	statefulSetPath := getChartPathForStatefulSet()
// 	chartPath := getChartPath()
// 	renderedChart, err := r.chartApplier.Render(chartPath, etcd.Name, etcd.Namespace, values)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if _, ok := renderedChart.Files()[statefulSetPath]; !ok {
// 		return nil, fmt.Errorf("missing configmap template file in the charts: %v", statefulSetPath)
// 	}

// 	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(renderedChart.Files()[statefulSetPath])), 1024)
// 	if err = decoder.Decode(&decoded); err != nil {
// 		return nil, err
// 	}
// 	return decoded, nil
// }
