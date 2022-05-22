/*
Copyright (c) 2022 VMware, Inc. All Rights Reserved.

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

package simulator_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vsan"
	_ "github.com/vmware/govmomi/vsan/simulator"
	"github.com/vmware/govmomi/vsan/types"
)

func TestPerformanceManager(t *testing.T) {
	simulator.Test(func(ctx context.Context, c *vim25.Client) {
		vsanClient, err := vsan.NewClient(ctx, c)
		if err != nil {
			t.Fatal(err)
		}

		finder := find.NewFinder(c)
		cluster, err := finder.DefaultClusterComputeResource(ctx)
		if err != nil {
			t.Fatal(err)
		}

		querySpec := []types.VsanPerfQuerySpec{
			{
				EntityRefId: "virtual-machine:*",
			},
		}

		clusterRef := cluster.Reference()
		csvs, err := vsanClient.VsanPerfQueryPerf(ctx, &clusterRef, querySpec)
		if err != nil {
			t.Fatal(err)
		}

		if len(csvs) == 0 {
			t.Errorf("csvs=%d", len(csvs))
		}
	})
}
