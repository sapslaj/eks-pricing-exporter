/*
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
package model_test

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sapslaj/eks-node-viewer-exporter/pkg/model"
)

func testNode(name string) *v1.Node {
	n := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: v1.NodeStatus{
			Phase: v1.NodePending,
		},
	}
	return n
}

func TestNewNode(t *testing.T) {
	n := testNode("mynode")
	node := model.NewNode(n)
	if exp, got := "mynode", node.Name(); exp != got {
		t.Errorf("expeted Name == %s, got %s", exp, got)
	}
}

func TestNodeTypeUnknown(t *testing.T) {
	n := testNode("mynode")
	node := model.NewNode(n)
	if node.IsOnDemand() {
		t.Errorf("exepcted to not be on-demand")
	}
	if node.IsSpot() {
		t.Errorf("exepcted to not be spot")
	}
}

func TestNodeTypeOnDemand(t *testing.T) {
	for label, value := range map[string]string{
		"karpenter.sh/capacity-type":     "on-demand",
		"eks.amazonaws.com/capacityType": "ON_DEMAND",
	} {
		n := testNode("mynode")
		n.Labels = map[string]string{
			label: value,
		}
		node := model.NewNode(n)
		if !node.IsOnDemand() {
			t.Errorf("exepcted on-demand")
		}
		if node.IsSpot() {
			t.Errorf("exepcted to not be spot")
		}
		if node.IsFargate() {
			t.Errorf("exepcted to not be fargate")
		}
	}
}

func TestNodeTypeSpot(t *testing.T) {
	for label, value := range map[string]string{
		"karpenter.sh/capacity-type":     "spot",
		"eks.amazonaws.com/capacityType": "SPOT",
	} {
		n := testNode("mynode")
		n.Labels = map[string]string{
			label: value,
		}
		node := model.NewNode(n)
		if node.IsOnDemand() {
			t.Errorf("exepcted to not be on-demand")
		}
		if !node.IsSpot() {
			t.Errorf("exepcted to be spot")
		}
		if node.IsFargate() {
			t.Errorf("exepcted to not be fargate")
		}
	}
}

func TestNodeTypeFargate(t *testing.T) {
	for label, value := range map[string]string{
		"eks.amazonaws.com/compute-type": "fargate",
	} {
		n := testNode("mynode")
		n.Labels = map[string]string{
			label: value,
		}
		node := model.NewNode(n)
		if node.IsOnDemand() {
			t.Errorf("exepcted to not be on-demand")
		}
		if node.IsSpot() {
			t.Errorf("exepcted to not be spot")
		}
		if !node.IsFargate() {
			t.Errorf("exepcted to be fargate")
		}
	}
}
