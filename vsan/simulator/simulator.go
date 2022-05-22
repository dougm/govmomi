/*
Copyright (c) 2021 VMware, Inc. All Rights Reserved.

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

package simulator

import (
	"strings"

	"github.com/google/uuid"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	vim "github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/govmomi/vsan"
	"github.com/vmware/govmomi/vsan/methods"
	"github.com/vmware/govmomi/vsan/types"
)

func init() {
	simulator.RegisterEndpoint(func(s *simulator.Service, r *simulator.Registry) {
		if r.IsVPX() {
			s.RegisterSDK(New())
		}
	})
}

func New() *simulator.Registry {
	r := simulator.NewRegistry()
	r.Namespace = vsan.Namespace
	r.Path = vsan.Path

	r.Put(&StretchedClusterSystem{
		ManagedObjectReference: vsan.VsanVcStretchedClusterSystem,
	})

	r.Put(&ClusterConfigSystem{
		ManagedObjectReference: vsan.VsanVcClusterConfigSystemInstance,
	})

	r.Put(&PerformanceManager{
		ManagedObjectReference: vsan.VsanPerformanceManagerInstance,
	})

	return r
}

type StretchedClusterSystem struct {
	vim.ManagedObjectReference
}

func (s *StretchedClusterSystem) VSANVcConvertToStretchedCluster(ctx *simulator.Context, req *types.VSANVcConvertToStretchedCluster) soap.HasFault {
	task := simulator.CreateTask(s, "convertToStretchedCluster", func(*simulator.Task) (vim.AnyType, vim.BaseMethodFault) {
		// TODO: validate req fields
		return nil, nil
	})

	return &methods.VSANVcConvertToStretchedClusterBody{
		Res: &types.VSANVcConvertToStretchedClusterResponse{
			Returnval: task.Run(ctx),
		},
	}
}

type ClusterConfigSystem struct {
	vim.ManagedObjectReference

	Config map[vim.ManagedObjectReference]*types.VsanConfigInfoEx
}

func (s *ClusterConfigSystem) info(ref vim.ManagedObjectReference) *types.VsanConfigInfoEx {
	if s.Config == nil {
		s.Config = make(map[vim.ManagedObjectReference]*types.VsanConfigInfoEx)
	}

	info := s.Config[ref]
	if info == nil {
		info = &types.VsanConfigInfoEx{}
		info.DefaultConfig = &vim.VsanClusterConfigInfoHostDefaultInfo{
			Uuid: uuid.New().String(),
		}
		s.Config[ref] = info
	}

	return info
}

func (s *ClusterConfigSystem) VsanClusterGetConfig(ctx *simulator.Context, req *types.VsanClusterGetConfig) soap.HasFault {
	return &methods.VsanClusterGetConfigBody{
		Res: &types.VsanClusterGetConfigResponse{
			Returnval: *s.info(req.Cluster),
		},
	}
}

func (s *ClusterConfigSystem) VsanClusterReconfig(ctx *simulator.Context, req *types.VsanClusterReconfig) soap.HasFault {
	task := simulator.CreateTask(s, "vsanClusterReconfig", func(*simulator.Task) (vim.AnyType, vim.BaseMethodFault) {
		// TODO: validate req fields
		info := s.info(req.Cluster)
		if req.VsanReconfigSpec.UnmapConfig != nil {
			info.UnmapConfig = req.VsanReconfigSpec.UnmapConfig
		}
		return nil, nil
	})

	return &methods.VsanClusterReconfigBody{
		Res: &types.VsanClusterReconfigResponse{
			Returnval: task.Run(ctx),
		},
	}
}

type PerformanceManager struct {
	vim.ManagedObjectReference
}

func (m *PerformanceManager) virtualMachines(ctx *simulator.Context, root vim.ManagedObjectReference, filter property.Filter) []mo.VirtualMachine {
	kind := []string{"VirtualMachine"}

	v, err := view.NewManager(ctx.Client()).CreateContainerView(ctx, root, kind, true)
	if err != nil {
		panic(err)
	}

	var vms []mo.VirtualMachine
	err = v.RetrieveWithFilter(ctx, kind, []string{"config.uuid"}, &vms, filter)
	if err != nil {
		panic(err)
	}

	return vms
}

var metrics = map[string][]string{
	"virtual-machine": {
		"iopsRead", "throughputRead", "latencyRead", "readCount",
		"iopsWrite", "throughputWrite", "latencyWrite", "writeCount",
	},
}

func (m *PerformanceManager) VsanPerfQueryPerf(ctx *simulator.Context, req *types.VsanPerfQueryPerf) soap.HasFault {
	body := new(methods.VsanPerfQueryPerfBody)

	var res []types.VsanPerfEntityMetricCSV

	for _, spec := range req.QuerySpecs {
		filter := property.Filter{}

		id := strings.SplitN(spec.EntityRefId, ":", 2)
		kind, uuid := id[0], id[1]
		ids := metrics[kind]

		switch kind {
		case "virtual-machine":
			if uuid != "*" {
				filter["config.uuid"] = uuid
			}
			vms := m.virtualMachines(ctx, *req.Cluster, filter)
			for _, vm := range vms {
				csv := types.VsanPerfEntityMetricCSV{
					EntityRefId: vm.Config.Uuid,
					SampleInfo:  "", // TODO
				}

				for _, id := range ids {
					val := types.VsanPerfMetricSeriesCSV{
						MetricId: types.VsanPerfMetricId{
							Label:                  id,
							MetricsCollectInterval: 300,
						},
						Values: "", // TODO
					}

					csv.Value = append(csv.Value, val)
				}

				res = append(res, csv)
			}
		}
	}

	body.Res = &types.VsanPerfQueryPerfResponse{Returnval: res}

	return body
}
