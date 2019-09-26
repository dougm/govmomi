/*
Copyright (c) 2014-2015 VMware, Inc. All Rights Reserved.

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

package tree

import (
	"context"
	"flag"
	"os"
	gopath "path"
	"time"

	gotree "github.com/a8m/tree"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/govc/cli"
	"github.com/vmware/govmomi/govc/flags"
	"github.com/vmware/govmomi/list"
	"github.com/vmware/govmomi/vim25/types"
)

type tree struct {
	*flags.DatacenterFlag

	ToRef bool
	DeRef bool
}

func init() {
	cli.Register("tree", &tree{})
}

func (cmd *tree) Register(ctx context.Context, f *flag.FlagSet) {
	cmd.DatacenterFlag, ctx = flags.NewDatacenterFlag(ctx)
	cmd.DatacenterFlag.Register(ctx, f)

	f.BoolVar(&cmd.ToRef, "i", false, "Print the managed object reference")
	f.BoolVar(&cmd.DeRef, "L", false, "Follow managed object references")
}

func (cmd *tree) Description() string {
	return `List contents of the inventory in a tree-like format.

Examples:
  govc tree /
  govc tree -L Folder:group-v22
  govc tree -i /datacenter/vm`
}

func (cmd *tree) Process(ctx context.Context) error {
	if err := cmd.DatacenterFlag.Process(ctx); err != nil {
		return err
	}
	return nil
}

func (cmd *tree) Usage() string {
	return "[PATH]..."
}

func (cmd *tree) Run(ctx context.Context, f *flag.FlagSet) error {
	finder, err := cmd.Finder(cmd.All())
	if err != nil {
		return err
	}

	args := f.Args()
	if len(args) == 0 {
		args = []string{"/"}
	}

	vfs := &virtualFileSystem{
		ctx:    context.Background(),
		cmd:    cmd,
		finder: finder,
	}

	treeOpts := &gotree.Options{
		Fs:      vfs,
		OutFile: os.Stdout,
	}

	var nd, nf int
	for i := range args {
		inf := gotree.New(args[i])
		d, f := inf.Visit(treeOpts)
		nd, nf = nd+d, nf+f
		inf.Print(treeOpts)
	}

	return nil
}

type virtualFileSystem struct {
	ctx    context.Context
	cmd    *tree
	finder *find.Finder
}

func (vfs virtualFileSystem) Stat(path string) (os.FileInfo, error) {
	var (
		listElement *list.Element
		ref         = &types.ManagedObjectReference{}
	)
	if vfs.cmd.DeRef && ref.FromString(path) {
		if e, err := vfs.finder.Element(vfs.ctx, *ref); err == nil {
			listElement = e
		}
	}
	if listElement == nil {
		if result, err := vfs.finder.ManagedObjectList(vfs.ctx, path); err == nil {
			if len(result) == 1 {
				listElement = &result[0]
			}
		}
	}
	if listElement == nil {
		return nil, os.ErrNotExist
	}

	var mode os.FileMode
	switch listElement.Object.Reference().Type {
	case "ComputeResource",
		"ClusterComputeResource",
		"Datacenter",
		"Folder",
		"HostSystem",
		"VirtualApp",
		"StoragePod":
		mode = mode | os.ModeDir
	}

	return fileInfo{name: gopath.Base(listElement.Path), mode: mode}, nil
}

func (vfs virtualFileSystem) ReadDir(path string) ([]string, error) {
	children, err := vfs.finder.ManagedObjectListChildren(vfs.ctx, path)
	if err != nil {
		return nil, err
	}
	childPaths := make([]string, len(children))
	for i := range children {
		childPaths[i] = gopath.Base(children[i].Path)
	}
	return childPaths, nil
}

type fileInfo struct {
	name string
	mode os.FileMode
}

func (f fileInfo) Name() string {
	return f.name
}
func (f fileInfo) Size() int64 {
	return 0
}
func (f fileInfo) Mode() os.FileMode {
	return f.mode
}
func (f fileInfo) ModTime() time.Time {
	return time.Now()
}
func (f fileInfo) IsDir() bool {
	return f.mode&os.ModeDir == os.ModeDir
}
func (f fileInfo) Sys() interface{} {
	return nil
}
