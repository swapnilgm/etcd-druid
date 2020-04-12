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

const backupHealthEndpoint = "/backup"

// type backupHealthzChecker struct {
// 	etcd         *druidv1alpha1.Etcd
// 	lastVerified atomic.Value
// }

// func (b *backupHealthzChecker) Check() error {
// 	return nil
// }

// func backupHealthChecker(req *http.Request) error {
// 	reqPath := req.URL.Path
// 	if reqPath == "" || reqPath[0] != '/' {
// 		reqPath = "/" + reqPath
// 	}
// 	// path.Clean removes the trailing slash except for root for us
// 	// (which is fine, since we're only serving one layer of sub-paths)
// 	reqPath = path.Clean(reqPath)

// 	// either serve the root endpoint...
// 	if strings.HasPrefix(reqPath, fmt.Sprintf(backupHealthEndpoint, "/")) {
// 		reqPath = reqPath[len(backupHealthEndpoint):]
// 	}

// 	// // ...the default check (if nothing else is present)...
// 	// if len(h.Checks) == 0 && reqPath[1:] == "ping" {
// 	// 	CheckHandler{Checker: Ping}.ServeHTTP(resp, req)
// 	// 	return
// 	// }
// 	return nil
// }
