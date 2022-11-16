/*
 * Copyright 2022 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package weightroundrobin

import (
	"sync"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app/client/discovery"
	"github.com/cloudwego/hertz/pkg/app/client/loadbalance"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"golang.org/x/sync/singleflight"
)

type weightRoundRobinBalancer struct {
	cachedInfo sync.Map
	sfg        singleflight.Group
}

type weightRoundRobinInfo struct {
	instances       []discovery.Instance
	effectiveWeight []int32
	currentWeight   []int32
	weightSum       int
}

// NewWeightRoundRobinBalancer creates a loadbalancer using round-robin algorithm.
func NewWeightRoundRobinBalancer() loadbalance.Loadbalancer {
	lb := &weightRoundRobinBalancer{}
	return lb
}

func (rr *weightRoundRobinBalancer) calcWeightInfo(e discovery.Result) *weightRoundRobinInfo {
	w := &weightRoundRobinInfo{
		instances:       make([]discovery.Instance, len(e.Instances)),
		effectiveWeight: make([]int32, len(e.Instances)),
		currentWeight:   make([]int32, len(e.Instances)),
		weightSum:       0,
	}

	var cnt int

	for idx := range e.Instances {
		weight := e.Instances[idx].Weight()
		if weight > 0 {
			w.instances[cnt] = e.Instances[idx]
			w.effectiveWeight[cnt] = int32(weight)
			w.currentWeight[cnt] = 0
			w.weightSum += weight
			cnt++
		} else {
			hlog.SystemLogger().Warnf("Invalid weight=%d on instance address=%s", weight, e.Instances[idx].Address())
		}
	}

	w.instances = w.instances[:cnt]
	return w
}

// Pick implements the Loadbalancer interface.
func (rr *weightRoundRobinBalancer) Pick(e discovery.Result) discovery.Instance {
	ri, ok := rr.cachedInfo.Load(e.CacheKey)
	if !ok {
		ri, _, _ = rr.sfg.Do(e.CacheKey, func() (interface{}, error) {
			return rr.calcWeightInfo(e), nil
		})
		rr.cachedInfo.Store(e.CacheKey, ri)
	}

	r := ri.(*weightRoundRobinInfo)
	if r.weightSum <= 0 {
		return nil
	}

	var bestIdx int
	for idx := range e.Instances {
		atomic.AddInt32(&r.currentWeight[idx], r.effectiveWeight[idx])
		// Pick the index with the biggest weight
		if r.currentWeight[bestIdx] < r.currentWeight[idx] {
			bestIdx = idx
		}
	}

	r.currentWeight[bestIdx] -= int32(r.weightSum)
	return e.Instances[bestIdx]
}

// Rebalance implements the Loadbalancer interface.
func (rr *weightRoundRobinBalancer) Rebalance(e discovery.Result) {
	rr.cachedInfo.Store(e.CacheKey, rr.calcWeightInfo(e))
}

// Delete implements the Loadbalancer interface.
func (rr *weightRoundRobinBalancer) Delete(cacheKey string) {
	rr.cachedInfo.Delete(cacheKey)
}

// Name implements the Loadbalancer interface.
func (rr *weightRoundRobinBalancer) Name() string {
	return "weight_round_robin"
}
