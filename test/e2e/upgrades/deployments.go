/*
Copyright 2017 The Kubernetes Authors.

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

package upgrades

import (
	"fmt"

	extensions "k8s.io/kubernetes/pkg/apis/extensions/v1beta1"
	deploymentutil "k8s.io/kubernetes/pkg/controller/deployment/util"
	"k8s.io/kubernetes/test/e2e/framework"

	. "github.com/onsi/ginkgo"
)

// TODO: Test that the deployment stays available during master (and maybe
// node and cluster upgrades).

// DeploymentUpgradeTest tests that a deployment is using the same replica
// sets before and after a cluster upgrade.
type DeploymentUpgradeTest struct {
	d     *extensions.Deployment
	oldRS *extensions.ReplicaSet
	newRS *extensions.ReplicaSet
}

// Setup creates a deployment and makes sure it has a new and an old replica set running.
func (t *DeploymentUpgradeTest) Setup(f *framework.Framework) {
	deploymentName := "deployment-hash-test"
	c := f.ClientSet
	nginxImage := "gcr.io/google_containers/nginx-slim:0.8"

	// Grab a unique namespace so we don't collide.
	namespace, err := f.CreateNamespace("deployment-upgrade", nil)
	framework.ExpectNoError(err)
	ns := namespace.Name

	By(fmt.Sprintf("Creating a deployment %q in namespace %q", deploymentName, ns))
	d := framework.NewDeployment(deploymentName, int32(1), map[string]string{"test": "upgrade"}, "nginx", nginxImage, extensions.RollingUpdateDeploymentStrategyType)
	deployment, err := c.Extensions().Deployments(ns).Create(d)
	framework.ExpectNoError(err)

	// Wait for it to be updated to revision 1
	By(fmt.Sprintf("Waiting deployment %q to be updated to revision 1", deploymentName))
	err = framework.WaitForDeploymentRevisionAndImage(c, ns, deploymentName, "1", nginxImage)
	framework.ExpectNoError(err)

	By(fmt.Sprintf("Waiting deployment %q to complete", deploymentName))
	framework.ExpectNoError(framework.WaitForDeploymentStatusValid(c, deployment))

	rs, err := deploymentutil.GetNewReplicaSet(deployment, c)
	framework.ExpectNoError(err)
	if rs == nil {
		framework.ExpectNoError(fmt.Errorf("expected a new replica set for deployment %q, found none", deployment.Name))
	}

	// Store the old replica set - should be the same after the upgrade.
	t.oldRS = rs

	// Trigger a new rollout so that we have some history.
	By(fmt.Sprintf("Triggering a new rollout for deployment %q", deploymentName))
	deployment, err = framework.UpdateDeploymentWithRetries(c, ns, deploymentName, func(update *extensions.Deployment) {
		update.Spec.Template.Spec.Containers[0].Name = "updated-name"
	})
	framework.ExpectNoError(err)

	// Use observedGeneration to determine if the controller noticed the pod template update.
	framework.Logf("Wait deployment %q to be observed by the deployment controller", deploymentName)
	framework.ExpectNoError(framework.WaitForObservedDeployment(c, ns, deploymentName, deployment.Generation))

	// Wait for it to be updated to revision 2
	By(fmt.Sprintf("Waiting deployment %q to be updated to revision 2", deploymentName))
	framework.ExpectNoError(framework.WaitForDeploymentRevisionAndImage(c, ns, deploymentName, "2", nginxImage))

	By(fmt.Sprintf("Waiting deployment %q to complete", deploymentName))
	framework.ExpectNoError(framework.WaitForDeploymentStatus(c, deployment))

	rs, err = deploymentutil.GetNewReplicaSet(deployment, c)
	framework.ExpectNoError(err)
	if rs == nil {
		framework.ExpectNoError(fmt.Errorf("expected a new replica set for deployment %q", deployment.Name))
	}

	if rs.UID == t.oldRS.UID {
		framework.ExpectNoError(fmt.Errorf("expected a new replica set different from the previous one"))
	}

	// Store new replica set - should be the same after the upgrade.
	t.newRS = rs
	t.d = deployment
}

// Test checks whether the replica sets for a deployment are the same after an upgrade.
func (t *DeploymentUpgradeTest) Test(f *framework.Framework, done <-chan struct{}, upgrade UpgradeType) {
	// Block until upgrade is done
	By(fmt.Sprintf("Waiting for upgrade to finish before checking replica sets for deployment %q", t.d.Name))
	<-done

	By(fmt.Sprintf("Checking that replica sets for deployment %q are the same as prior to the upgrade", t.d.Name))
	_, allOldRSs, newRS, err := deploymentutil.GetAllReplicaSets(t.d, f.ClientSet)
	framework.ExpectNoError(err)
	if newRS == nil {
		framework.ExpectNoError(fmt.Errorf("expected a new replica set for deployment %q", t.d.Name))
	}
	if newRS.UID != t.newRS.UID {
		framework.ExpectNoError(fmt.Errorf("expected new replica set:\n%#v\ngot new replica set:\n%#v\n", t.newRS, newRS))
	}
	if len(allOldRSs) != 1 {
		errString := fmt.Sprintf("expected one old replica set, got %d\n", len(allOldRSs))
		for i := range allOldRSs {
			rs := allOldRSs[i]
			errString += fmt.Sprintf("%#v\n", rs)
		}
		framework.ExpectNoError(fmt.Errorf(errString))
	}
	if allOldRSs[0].UID != t.oldRS.UID {
		framework.ExpectNoError(fmt.Errorf("expected old replica set:\n%#v\ngot old replica set:\n%#v\n", t.oldRS, allOldRSs[0]))
	}
}

// Teardown cleans up any remaining resources.
func (t *DeploymentUpgradeTest) Teardown(f *framework.Framework) {
	// rely on the namespace deletion to clean up everything
}
