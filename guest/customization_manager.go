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

package guest

import (
	"context"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/types"
)

type CustomizationManager struct {
	c  *vim25.Client
	vm types.ManagedObjectReference
}

func NewCustomizationManager(c *vim25.Client, vm types.ManagedObjectReference) *CustomizationManager {
	return &CustomizationManager{c, vm}
}

func (m *CustomizationManager) Customize(ctx context.Context, auth types.BaseGuestAuthentication, spec types.CustomizationSpec) (*object.Task, error) {
	ref := m.c.ServiceContent.GuestCustomizationManager
	if ref == nil {
		return nil, object.ErrNotSupported
	}

	req := types.CustomizeGuest_Task{
		This: *ref,
		Vm:   m.vm,
		Auth: auth,
		Spec: spec,
	}

	res, err := methods.CustomizeGuest_Task(ctx, m.c, &req)
	if err != nil {
		return nil, err
	}

	return object.NewTask(m.c, res.Returnval), nil
}
