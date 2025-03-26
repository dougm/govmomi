// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"flag"

	"github.com/vmware/govmomi/cli"
	"github.com/vmware/govmomi/cli/flags"
	"github.com/vmware/govmomi/cli/resource"
)

type usage struct {
	*flags.DatacenterFlag

	shared   bool
	interval string
}

func init() {
	cli.Register("cluster.usage", &usage{})
}

func (cmd *usage) Register(ctx context.Context, f *flag.FlagSet) {
	cmd.DatacenterFlag, ctx = flags.NewDatacenterFlag(ctx)
	cmd.DatacenterFlag.Register(ctx, f)

	f.BoolVar(&cmd.shared, "S", false, "Exclude host local storage")
	f.StringVar(&cmd.interval, "i", "", "Historical interval (day|week|month|year)")
}

func (cmd *usage) Usage() string {
	return "CLUSTER"
}

func (cmd *usage) Description() string {
	return `Cluster resource usage summary.

Examples:
  govc cluster.usage ClusterName
  govc cluster.usage -i month ClusterName # usage for past month
  govc cluster.usage -S ClusterName # summarize shared storage only
  govc cluster.usage -json ClusterName | jq -r .cpu.summary.usage`
}

func (cmd *usage) Run(ctx context.Context, f *flag.FlagSet) error {
	c, err := cmd.Client()
	if err != nil {
		return err
	}

	finder, err := cmd.Finder()
	if err != nil {
		return err
	}

	obj, err := finder.ClusterComputeResourceOrDefault(ctx, f.Arg(0))
	if err != nil {
		return err
	}

	s := resource.Cluster{
		ManagedObjectReference: obj.Reference(),
		Storage:                true,
		SharedStorage:          cmd.shared,
	}

	res, err := s.Usage(ctx, c, cmd.interval)
	if err != nil {
		return err
	}

	return cmd.WriteResult(res)
}
