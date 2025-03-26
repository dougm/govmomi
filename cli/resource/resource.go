// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type Cluster struct {
	types.ManagedObjectReference

	Storage       bool
	SharedStorage bool

	cluster    mo.ClusterComputeResource
	hosts      []mo.HostSystem
	datastores []mo.Datastore
}

type UsageSummary struct {
	Used     string
	Free     string
	Capacity string
	Usage    string
}

type Usage struct {
	Used     int64
	Free     int64
	Capacity int64
	Usage    float64
	Summary  UsageSummary
}

type ProcessorUsage struct {
	Usage
	Speed int32
	Cores int
}

type Summary struct {
	Memory  Usage
	CPU     ProcessorUsage
	Storage *Usage
}

func (r *Cluster) Usage(ctx context.Context, c *vim25.Client, interval string) (*Summary, error) {
	pc := property.DefaultCollector(c)

	err := pc.RetrieveOne(ctx, r.ManagedObjectReference, []string{"datastore", "host"}, &r.cluster)
	if err != nil {
		return nil, err
	}

	props := []string{
		"summary.hardware.numCpuCores",
		"summary.hardware.cpuMhz",
		"summary.hardware.memorySize",
		"summary.quickStats",
	}

	err = pc.Retrieve(ctx, r.cluster.Host, props, &r.hosts)
	if err != nil {
		return nil, err
	}

	var res Summary

	if r.Storage || r.SharedStorage {
		res.Storage = new(Usage)
		var ds []mo.Datastore
		err = pc.Retrieve(ctx, r.cluster.Datastore, []string{"summary"}, &ds)
		if err != nil {
			return nil, err
		}

		r.cluster.Datastore = nil
		for _, datastore := range ds {
			shared := datastore.Summary.MultipleHostAccess
			if r.SharedStorage && shared != nil && *shared == false {
				continue
			}
			r.datastores = append(r.datastores, datastore)
			r.cluster.Datastore = append(r.cluster.Datastore, datastore.Self)
		}
	}

	r.current(&res)
	if interval != "" {
		err = r.history(ctx, c, &res, interval)
		if err != nil {
			return nil, err
		}
	}

	res.CPU.Free = res.CPU.Capacity - res.CPU.Used
	res.CPU.summarize(ghz)

	res.Memory.Free = res.Memory.Capacity - res.Memory.Used
	res.Memory.summarize(size)

	if res.Storage != nil {
		res.Storage.Used = res.Storage.Capacity - res.Storage.Free
		res.Storage.summarize(size)
	}

	return &res, nil
}

func (r *Cluster) current(res *Summary) {
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

func newSpec(interval string) (types.PerfQuerySpec, error) {
	spec := types.PerfQuerySpec{
		Format:    string(types.PerfFormatNormal),
		MaxSample: int32(365),
		MetricId:  []types.PerfMetricId{{Instance: ""}}, // aggregate instance only
	}

	spec.IntervalId = performance.Intervals[interval]
	if spec.IntervalId == 0 {
		return spec, fmt.Errorf("invalid interval: %q", interval)
	}

	return spec, nil
}

func (r *Cluster) history(ctx context.Context, c *vim25.Client, res *Summary, interval string) error {
	pm := performance.NewManager(c)

	spec, err := newSpec(interval)
	if err != nil {
		return err
	}

	objs := []types.ManagedObjectReference{r.cluster.Self}
	names := []string{
		"cpu.totalmhz.average",
		"cpu.usagemhz.average",
		"mem.totalmb.average",
		"mem.consumed.average",
		"mem.overhead.average",
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
		res.Memory.Used = 0
		for _, v := range result[0].Value {
			switch v.Name {
			case "cpu.totalmhz.average":
				res.CPU.Capacity = v.Average()
			case "cpu.usagemhz.average":
				res.CPU.Used = v.Average()
			case "mem.totalmb.average":
				res.Memory.Free = v.Average() << 20
			case "mem.consumed.average":
				res.Memory.Used += v.Average() << 10
			case "mem.overhead.average":
				res.Memory.Used += v.Average() << 10
			}
		}

		res.CPU.Cores = int(res.CPU.Capacity / int64(res.CPU.Speed))
	}
	res.Memory.Capacity = res.Memory.Free + res.Memory.Used

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

func (r *Usage) summarize(f func(int64) string) {
	r.Usage = 100 * float64(r.Used) / float64(r.Capacity)

	r.Summary.Usage = fmt.Sprintf("%.1f", r.Usage)
	r.Summary.Capacity = f(r.Capacity)
	r.Summary.Used = f(r.Used)
	r.Summary.Free = f(r.Free)
}

func (r *Usage) write(w io.Writer, label string) {
	fmt.Fprintf(w, "%s usage:\t%s%%\n", label, r.Summary.Usage)
	fmt.Fprintf(w, "%s capacity:\t%s\n", label, r.Summary.Capacity)
	fmt.Fprintf(w, "%s used:\t%s\n", label, r.Summary.Used)
	fmt.Fprintf(w, "%s free:\t%s\n", label, r.Summary.Free)
}

func ghz(val int64) string {
	return fmt.Sprintf("%.2fGHz", float64(val)/1000)
}

func size(val int64) string {
	return units.ByteSize(val).String()
}

func (r *Summary) Write(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)

	r.CPU.write(tw, "CPU")
	cores := r.CPU.Used / int64(r.CPU.Speed)
	if cores == 0 {
		cores = 1
	}
	fmt.Fprintf(tw, "CPU cores used:\t%d/%d\n", cores, r.CPU.Cores)
	fmt.Fprintf(tw, "\t\n")

	r.Memory.write(tw, "Memory")
	fmt.Fprintf(tw, "\t\n")

	if r.Storage != nil {
		r.Storage.write(tw, "Storage")
	}

	return tw.Flush()
}
