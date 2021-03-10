/*
Copyright (c) 2020 VMware, Inc. All Rights Reserved.

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

package cluster

import (
	"context"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/vmware/govmomi/govc/cli"
	"github.com/vmware/govmomi/govc/flags"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type usage struct {
	*flags.DatacenterFlag

	shared bool
	past   string
}

func init() {
	cli.Register("cluster.usage", &usage{})
}

func (cmd *usage) Register(ctx context.Context, f *flag.FlagSet) {
	cmd.DatacenterFlag, ctx = flags.NewDatacenterFlag(ctx)
	cmd.DatacenterFlag.Register(ctx, f)

	f.BoolVar(&cmd.shared, "S", false, "Exclude host local storage")
	f.StringVar(&cmd.past, "i", "", "Usage for the past day, week, month or year")
}

func (cmd *usage) Usage() string {
	return "CLUSTER"
}

func (cmd *usage) Description() string {
	return `Cluster resource usage summary.

Examples:
  govc cluster.usage ClusterName
  govc cluster.usage -S ClusterName # summarize shared storage only
  govc cluster.usage -json ClusterName | jq -r .CPU.Summary.Usage`
}

type resources struct {
	cluster    mo.ClusterComputeResource
	hosts      []mo.HostSystem
	datastores []mo.Datastore
}

func (r *resources) current(res *Usage) {
	for _, host := range r.hosts {
		res.CPU.Capacity += int64(int32(host.Summary.Hardware.NumCpuCores) * host.Summary.Hardware.CpuMhz)
		res.CPU.Used += int64(host.Summary.QuickStats.OverallCpuUsage)
		res.CPU.Cores += int(host.Summary.Hardware.NumCpuCores)
		res.CPU.Speed = host.Summary.Hardware.CpuMhz // TODO: average

		res.Memory.Capacity += host.Summary.Hardware.MemorySize
		res.Memory.Used += int64(host.Summary.QuickStats.OverallMemoryUsage) << 20
	}

	for _, datastore := range r.datastores {
		res.Storage.Capacity += datastore.Summary.Capacity
		res.Storage.Free += datastore.Summary.FreeSpace
	}
}

func (r *resources) history(ctx context.Context, c *vim25.Client, res *Usage, past string) error {
	pm := performance.NewManager(c)

	spec := types.PerfQuerySpec{
		Format:    string(types.PerfFormatNormal),
		MaxSample: int32(365),
		MetricId:  []types.PerfMetricId{{Instance: ""}}, // aggregate instance only
	}

	spec.IntervalId = performance.Intervals[past]
	if spec.IntervalId == 0 {
		return fmt.Errorf("invalid interval: -past %q", past)
	}

	objs := []types.ManagedObjectReference{r.cluster.Self}
	names := []string{
		"cpu.totalmhz.average",
		"cpu.usagemhz.average",
		"mem.totalmb.average",
		"mem.consumed.average",
	}

	sample, err := pm.SampleByName(ctx, spec, names, objs)
	if err != nil {
		return err
	}

	result, err := pm.ToMetricSeries(ctx, sample)
	if err != nil {
		return err
	}

	if len(result) != 0 {
		for _, v := range result[0].Value {
			switch v.Name {
			case "cpu.totalmhz.average":
				res.CPU.Capacity = v.Average()
			case "cpu.usagemhz.average":
				res.CPU.Used = v.Average()
			case "mem.totalmb.average":
				res.Memory.Capacity = v.Average() << 20
			case "mem.consumed.average":
				res.Memory.Used = v.Average() << 10
			}
		}

		res.CPU.Cores = int(res.CPU.Capacity / int64(res.CPU.Speed))
	}

	names = []string{
		"disk.used.latest",
		"disk.capacity.latest",
	}

	sample, err = pm.SampleByName(ctx, spec, names, r.cluster.Datastore)
	if err != nil {
		return err
	}

	result, err = pm.ToMetricSeries(ctx, sample)
	if err != nil {
		return err
	}

	var used, capacity int64
	for _, r := range result {
		for _, v := range r.Value {
			switch v.Name {
			case "disk.used.latest":
				used += v.Average() << 10
			case "disk.capacity.latest":
				capacity += v.Average() << 10
			}
		}
	}
	res.Storage.Capacity = capacity
	res.Storage.Free = capacity - used

	return nil
}

func (cmd *usage) Run(ctx context.Context, f *flag.FlagSet) error {
	finder, err := cmd.Finder()
	if err != nil {
		return err
	}

	obj, err := finder.ClusterComputeResource(ctx, f.Arg(0))
	if err != nil {
		return err
	}

	var res Usage
	var u resources

	pc := property.DefaultCollector(obj.Client())

	err = pc.RetrieveOne(ctx, obj.Reference(), []string{"datastore", "host"}, &u.cluster)
	if err != nil {
		return err
	}

	props := []string{
		"summary.hardware.numCpuCores",
		"summary.hardware.cpuMhz",
		"summary.quickStats",
	}
	err = pc.Retrieve(ctx, u.cluster.Host, props, &u.hosts)
	if err != nil {
		return err
	}

	var ds []mo.Datastore
	err = pc.Retrieve(ctx, u.cluster.Datastore, []string{"summary"}, &ds)
	if err != nil {
		return err
	}

	u.cluster.Datastore = nil
	for _, datastore := range ds {
		shared := datastore.Summary.MultipleHostAccess
		if cmd.shared && shared != nil && *shared == false {
			continue
		}
		u.datastores = append(u.datastores, datastore)
		u.cluster.Datastore = append(u.cluster.Datastore, datastore.Self)
	}

	u.current(&res)
	if cmd.past != "" {
		err = u.history(ctx, obj.Client(), &res, cmd.past)
		if err != nil {
			return err
		}
	}

	res.CPU.Free = res.CPU.Capacity - res.CPU.Used
	res.CPU.summarize(ghz)

	res.Memory.Free = res.Memory.Capacity - res.Memory.Used
	res.Memory.summarize(size)

	res.Storage.Used = res.Storage.Capacity - res.Storage.Free
	res.Storage.summarize(size)

	return cmd.WriteResult(&res)
}

type ResourceUsageSummary struct {
	Used     string
	Free     string
	Capacity string
	Usage    string
}

type ResourceUsage struct {
	Used     int64
	Free     int64
	Capacity int64
	Usage    float64
	Summary  ResourceUsageSummary
}

func (r *ResourceUsage) summarize(f func(int64) string) {
	r.Usage = 100 * float64(r.Used) / float64(r.Capacity)

	r.Summary.Usage = fmt.Sprintf("%.1f", r.Usage)
	r.Summary.Capacity = f(r.Capacity)
	r.Summary.Used = f(r.Used)
	r.Summary.Free = f(r.Free)
}

func (r *ResourceUsage) write(w io.Writer, label string) {
	fmt.Fprintf(w, "%s usage:\t%s%%\n", label, r.Summary.Usage)
	fmt.Fprintf(w, "%s capacity:\t%s\n", label, r.Summary.Capacity)
	fmt.Fprintf(w, "%s used:\t%s\n", label, r.Summary.Used)
	fmt.Fprintf(w, "%s free:\t%s\n", label, r.Summary.Free)
}

func ghz(val int64) string {
	return fmt.Sprintf("%.1fGHz", float64(val)/1000)
}

func size(val int64) string {
	return units.ByteSize(val).String()
}

type ProcessorUsage struct {
	ResourceUsage
	Speed int32
	Cores int
}

type Usage struct {
	Memory  ResourceUsage
	CPU     ProcessorUsage
	Storage ResourceUsage
}

func (r *Usage) Write(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)

	r.CPU.write(tw, "CPU")
	fmt.Fprintf(tw, "CPU cores used:\t%d/%d\n", r.CPU.Used/int64(r.CPU.Speed), r.CPU.Cores)
	fmt.Fprintf(tw, "\t\n")

	r.Memory.write(tw, "Memory")
	fmt.Fprintf(tw, "\t\n")

	r.Storage.write(tw, "Storage")

	return tw.Flush()
}
