// Package objectclient provides retrying, dynamic.Interface-based create-or-update ("Apply"), Get, and
// delete-if-present ("Uninstall") helpers for arbitrary Kubernetes objects. It is deliberately
// dynamic-client-based rather than controller-runtime client.Client-based: callers that already carry a
// dynamic.Interface (e.g. because their own reconciliation model is schema-unknown-at-compile-time) can
// use it without adopting a second client type.
//
// Because objects are applied through the dynamic client, obj's GroupVersionKind must be set explicitly
// by the caller (TypeMeta) before calling Apply — a struct built via runtime.DefaultUnstructuredConverter
// or a bare unstructured.Unstructured literal often leaves TypeMeta empty, which makes the apply fail
// with "no kind is registered" or an equivalent low-level error.
package objectclient

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	retryAttempts     = 10
	retryInitialDelay = 100 * time.Millisecond
	retryMaximumDelay = 100 * time.Millisecond
)

// ApplyOptions mirrors metav1.CreateOptions/UpdateOptions, minus the fields Apply computes itself
// (Apply always uses the current resourceVersion for updates).
type ApplyOptions struct {
	// DryRun causes the request to be validated but not persisted. The only valid value is "All".
	DryRun []string
	// FieldManager is the name of the user or component submitting this request. Required for
	// server-side apply; recommended for ordinary Create/Update so field ownership is attributable.
	FieldManager string
	// FieldValidation controls how the server reacts to unknown/duplicate fields ("Ignore" | "Warn" |
	// "Strict").
	FieldValidation string
}

// UninstallOptions mirrors metav1.DeleteOptions.
type UninstallOptions struct {
	GracePeriodSeconds *int64
	Preconditions      *metav1.Preconditions
	PropagationPolicy  *metav1.DeletionPropagation
	DryRun             []string
}

// Apply creates obj if no object with its namespace/name exists yet, or updates it (carrying over the
// current resourceVersion) if one does. It retries on any transient error other than context
// cancellation, since the read-then-write is not atomic and can race a concurrent writer.
func Apply(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, obj *unstructured.Unstructured, opts ApplyOptions) error {
	res := namespacedOrCluster(dyn, gvr, obj.GetNamespace())
	name := obj.GetName()

	return retry(ctx, func(ctx context.Context) error {
		current, err := res.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err := res.Create(ctx, obj, metav1.CreateOptions{
					DryRun:          opts.DryRun,
					FieldManager:    opts.FieldManager,
					FieldValidation: opts.FieldValidation,
				})
				return err
			}
			return err
		}

		obj.SetResourceVersion(current.GetResourceVersion())
		_, err = res.Update(ctx, obj, metav1.UpdateOptions{
			DryRun:          opts.DryRun,
			FieldManager:    opts.FieldManager,
			FieldValidation: opts.FieldValidation,
		})
		return err
	})
}

// Get reads the object named name in namespace ns (cluster-scoped when ns is empty).
func Get(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	return namespacedOrCluster(dyn, gvr, ns).Get(ctx, name, metav1.GetOptions{})
}

// Uninstall deletes the object named name in namespace ns (cluster-scoped when ns is empty). A missing
// object, whether absent before the call or removed concurrently, is treated as success.
func Uninstall(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, ns, name string, opts UninstallOptions) error {
	res := namespacedOrCluster(dyn, gvr, ns)

	return retry(ctx, func(ctx context.Context) error {
		_, err := res.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		err = res.Delete(ctx, name, metav1.DeleteOptions{
			DryRun:             opts.DryRun,
			Preconditions:      opts.Preconditions,
			PropagationPolicy:  opts.PropagationPolicy,
			GracePeriodSeconds: opts.GracePeriodSeconds,
		})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
}

func namespacedOrCluster(dyn dynamic.Interface, gvr schema.GroupVersionResource, ns string) dynamic.ResourceInterface {
	if ns == "" {
		return dyn.Resource(gvr)
	}
	return dyn.Resource(gvr).Namespace(ns)
}

// retry runs op up to retryAttempts times, waiting retryInitialDelay (capped at retryMaximumDelay)
// between attempts, stopping early on success or context cancellation.
func retry(ctx context.Context, op func(context.Context) error) error {
	var err error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		err = op(ctx)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || attempt == retryAttempts {
			return err
		}

		delay := retryInitialDelay << (attempt - 1)
		if delay > retryMaximumDelay {
			delay = retryMaximumDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
