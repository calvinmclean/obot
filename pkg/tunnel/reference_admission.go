package tunnel

import (
	"context"

	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ReferenceAdmitter struct {
	storage  kclient.Client
	withLock func(context.Context, func() error) error
	next     func(context.Context, kclient.Object, func() error) error
}

// NewReferenceAdmitter uses uncached storage so checks see the writes made by
// the previous lock holder. next can enforce other admission policies.
func NewReferenceAdmitter(storage kclient.Client, withLock func(context.Context, func() error) error, next func(context.Context, kclient.Object, func() error) error) *ReferenceAdmitter {
	return &ReferenceAdmitter{storage: storage, withLock: withLock, next: next}
}

func (a *ReferenceAdmitter) Admit(ctx context.Context, obj kclient.Object, write func() error) error {
	publish := func() error {
		if a.next != nil {
			return a.next(ctx, obj, write)
		}
		return write()
	}
	var old kclient.Object
	switch obj := obj.(type) {
	case *v1.MCPServer:
		old = &v1.MCPServer{}
		if runtimeRoute(obj.Spec.Manifest).tunnel == "" {
			return publish()
		}
	case *v1.MCPServerCatalogEntry:
		old = &v1.MCPServerCatalogEntry{}
		if catalogRoute(obj.Spec.Manifest).tunnel == "" && assignmentRoute(*obj).tunnel == "" {
			return publish()
		}
	case *v1.VMCPInstance:
		old = &v1.VMCPInstance{}
		if len(obj.Spec.LegacyComponents) == 0 {
			return publish()
		}
	case *v1.VMCP:
		old = &v1.VMCP{}
		// An update can reactivate a retained connection snapshot even if
		// the vMCP's own components contain no tunnel references.
	default:
		return publish()
	}

	return a.withLock(ctx, func() error {
		wanted, err := a.references(ctx, obj)
		if err != nil {
			return err
		}
		if len(wanted) == 0 {
			return publish()
		}

		var previous map[routeReference]struct{}
		if obj.GetName() != "" {
			if err := a.storage.Get(ctx, kclient.ObjectKeyFromObject(obj), old); err == nil {
				previous, err = a.references(ctx, old)
				if err != nil {
					return err
				}
			} else if !apierrors.IsNotFound(err) {
				return err
			}
		}

		// An unrelated edit can leave an existing route in place even if it
		// was already invalid. Every newly published target must be allowed.
		for route := range wanted {
			if _, unchanged := previous[route]; unchanged {
				continue
			}
			if err := ValidateReference(ctx, a.storage, route.tunnel, route.target); err != nil {
				return apierrors.NewBadRequest(err.Error())
			}
		}
		return publish()
	})
}

func (a *ReferenceAdmitter) references(ctx context.Context, obj kclient.Object) (map[routeReference]struct{}, error) {
	refs := make(map[routeReference]struct{})
	add := func(ref routeReference) {
		if ref.tunnel != "" {
			refs[ref] = struct{}{}
		}
	}
	switch obj := obj.(type) {
	case *v1.MCPServer:
		add(runtimeRoute(obj.Spec.Manifest))
	case *v1.MCPServerCatalogEntry:
		add(catalogRoute(obj.Spec.Manifest))
		add(assignmentRoute(*obj))
	case *v1.VMCP:
		for _, component := range obj.Spec.Manifest.Components {
			add(catalogRoute(component.CatalogEntry.Manifest))
		}
		if obj.Name != "" {
			var instances v1.VMCPInstanceList
			if err := a.storage.List(ctx, &instances, kclient.InNamespace(obj.Namespace), kclient.MatchingFields{"spec.manifest.vmcpID": obj.Name}); err != nil {
				return nil, err
			}
			for _, instance := range instances.Items {
				for _, component := range retainedComponents(obj, &instance) {
					add(catalogRoute(component.CatalogEntry.Manifest))
				}
			}
		}
	case *v1.VMCPInstance:
		var definition v1.VMCP
		if err := a.storage.Get(ctx, kclient.ObjectKey{Namespace: obj.Namespace, Name: obj.Spec.Manifest.VMCPID}, &definition); err != nil {
			return nil, kclient.IgnoreNotFound(err)
		}
		for _, component := range retainedComponents(&definition, obj) {
			add(catalogRoute(component.CatalogEntry.Manifest))
		}
	}
	return refs, nil
}
