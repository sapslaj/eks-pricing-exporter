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

package model

import (
	"fmt"
	"math"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/sapslaj/eks-pricing-exporter/pkg/pricing"
)

type objectKey struct {
	namespace string
	name      string
}
type Node struct {
	mu      sync.RWMutex
	visible bool
	node    v1.Node
	pods    map[objectKey]*Pod
	used    v1.ResourceList
	Price   float64
}

type NodeCapacityType string

const (
	NodeUnknownCapacityType NodeCapacityType = ""
	NodeOnDemand            NodeCapacityType = "on-demand"
	NodeSpot                NodeCapacityType = "spot"
	NodeFargate             NodeCapacityType = "fargate"
)

func (nct NodeCapacityType) String() string {
	return string(nct)
}

type NodeStatus string

const (
	NodeStatusUnknown    NodeStatus = "Unknown"
	NodeCordonedDeleting NodeStatus = "Cordoned/Deleting"
	NodeDeleting         NodeStatus = "Deleting"
	NodeCordoned         NodeStatus = "Cordoned"
	NodeReady            NodeStatus = "Ready"
)

func (ns NodeStatus) String() string {
	return string(ns)
}

func NewNode(n *v1.Node) *Node {
	node := &Node{
		node: *n,
		pods: map[objectKey]*Pod{},
		used: v1.ResourceList{},
	}

	return node
}

func (n *Node) IsOnDemand() bool {
	return n.node.Labels["karpenter.sh/capacity-type"] == "on-demand" ||
		n.node.Labels["eks.amazonaws.com/capacityType"] == "ON_DEMAND"
}

func (n *Node) IsSpot() bool {
	return n.node.Labels["karpenter.sh/capacity-type"] == "spot" ||
		n.node.Labels["eks.amazonaws.com/capacityType"] == "SPOT"
}

func (n *Node) IsFargate() bool {
	return n.node.Labels["eks.amazonaws.com/compute-type"] == "fargate"
}

func (n *Node) CapacityType() NodeCapacityType {
	if n.IsOnDemand() {
		return NodeOnDemand
	} else if n.IsSpot() {
		return NodeSpot
	} else if n.IsFargate() {
		return NodeFargate
	} else {
		return NodeUnknownCapacityType
	}
}

func (n *Node) Status() NodeStatus {
	if n.Cordoned() && n.Deleting() {
		return NodeCordonedDeleting
	} else if n.Deleting() {
		return NodeDeleting
	} else if n.Cordoned() {
		return NodeCordoned
	} else if n.Ready() {
		return NodeReady
	} else {
		return NodeStatusUnknown
	}
}

func (n *Node) Update(node *v1.Node) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.node = *node
}

func (n *Node) Name() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.node.Name
}

func (n *Node) BindPod(pod *Pod) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := objectKey{
		namespace: pod.Namespace(),
		name:      pod.Name(),
	}
	_, alreadyBound := n.pods[key]
	n.pods[key] = pod

	if !alreadyBound {
		for rn, q := range pod.Requested() {
			existing := n.used[rn]
			existing.Add(q)
			n.used[rn] = existing
		}
	}
}

func (n *Node) DeletePod(namespace string, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := objectKey{namespace: namespace, name: name}
	if p, ok := n.pods[key]; ok {
		// subtract the pod requests
		for rn, q := range p.Requested() {
			existing := n.used[rn]
			existing.Sub(q)
			n.used[rn] = existing
		}
		delete(n.pods, key)
	}
}

func (n *Node) Allocatable() v1.ResourceList {
	n.mu.RLock()
	defer n.mu.RUnlock()
	// shouldn't be modified so it's safe to return
	return n.node.Status.Allocatable
}

func (n *Node) Used() v1.ResourceList {
	n.mu.RLock()
	defer n.mu.RUnlock()
	used := v1.ResourceList{}
	for rn, q := range n.used {
		used[rn] = q.DeepCopy()
	}
	return used
}

func (n *Node) Cordoned() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.node.Spec.Unschedulable
}

func (n *Node) Ready() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, c := range n.node.Status.Conditions {
		if c.Status == v1.ConditionTrue && c.Type == v1.NodeReady {
			return true
		}
	}
	return false
}

func (n *Node) Created() time.Time {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.node.CreationTimestamp.Time
}

func (n *Node) InstanceType() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.IsFargate() {
		if len(n.Pods()) == 1 {
			cpu, mem, ok := n.Pods()[0].FargateCapacityProvisioned()
			if ok {
				return fmt.Sprintf("%gvCPU-%gGB", cpu, mem)
			}
		}
		return "Fargate"
	}
	return n.node.Labels[v1.LabelInstanceTypeStable]
}

func (n *Node) Zone() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.node.Labels[v1.LabelTopologyZone]
}

func (n *Node) Region() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.node.Labels[v1.LabelTopologyRegion]
}

func (n *Node) NumPods() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.pods)
}

func (n *Node) Hide() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.visible = false
}

func (n *Node) Visible() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.visible
}

func (n *Node) Show() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.visible = true
}

func (n *Node) Deleting() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return !n.node.DeletionTimestamp.IsZero()
}

func (n *Node) Pods() []*Pod {
	var pods []*Pod
	for _, p := range n.pods {
		pods = append(pods, p)
	}
	return pods
}

func (n *Node) HasPrice() bool {
	// we use NaN for an unknown price, so if this is true the price is known
	return n.Price == n.Price
}

func (n *Node) UpdatePrice(pricingRepository *pricing.Repository) {
	// lookup our n price
	n.Price = math.NaN()
	if n.IsOnDemand() {
		if price, ok := pricingRepository.OnDemandPrice(n.InstanceType()); ok {
			n.Price = price
		}
	} else if n.IsSpot() {
		if price, ok := pricingRepository.SpotPrice(n.InstanceType(), n.Zone()); ok {
			n.Price = price
		}
	} else if n.IsFargate() && len(n.Pods()) == 1 {
		cpu, mem, ok := n.Pods()[0].FargateCapacityProvisioned()
		if ok {
			if price, ok := pricingRepository.FargatePrice(cpu, mem); ok {
				n.Price = price
			}
		}
	}
}
