package helm

import (
	"context"
	"reflect"
	"time"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/client-go/tools/cache"
)

type crdInformer struct {
	resyncTime  time.Duration
	apiExtCli   apiextclient.Interface
	invalidator interface{ Reset() }
	logger      func(format string, v ...interface{})
}

func NewCRDInformer(resyncTime time.Duration, apiExtCli apiextclient.Interface, invalidator interface{ Reset() }, logger func(format string, v ...interface{})) *crdInformer {
	return &crdInformer{
		resyncTime:  resyncTime,
		apiExtCli:   apiExtCli,
		invalidator: invalidator,
		logger:      logger,
	}
}

func (c *crdInformer) Start(ctx context.Context) error {
	if c.apiExtCli == nil {
		return nil
	}

	factory := apiextinformers.NewSharedInformerFactory(c.apiExtCli, c.resyncTime)
	crdInformer := factory.Apiextensions().V1().CustomResourceDefinitions().Informer()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if c.invalidator != nil {
				c.invalidator.Reset()
				if c.logger != nil {
					crd, ok := obj.(*apixv1.CustomResourceDefinition)
					if !ok {
						return
					}
					c.logger("discovery cache invalidated: CRD added (name: %s)", crd.Name)
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCRD, ok1 := oldObj.(*apixv1.CustomResourceDefinition)
			newCRD, ok2 := newObj.(*apixv1.CustomResourceDefinition)
			if !ok1 || !ok2 {
				return
			}

			// compare Specs: only invalidate if Spec actually changed
			if reflect.DeepEqual(oldCRD.Spec, newCRD.Spec) {
				// nothing meaningful changed
				return
			}

			if c.invalidator != nil {
				c.invalidator.Reset()
				if c.logger != nil {
					c.logger("discovery cache invalidated: CRD spec changed (name: %s)", newCRD.Name)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			if c.invalidator != nil {
				c.invalidator.Reset()
				if c.logger != nil {
					crd, ok := obj.(*apixv1.CustomResourceDefinition)
					if !ok {
						return
					}
					c.logger("discovery cache invalidated: CRD deleted (name: %s)", crd.Name)
				}
			}
		},
	}

	crdInformer.AddEventHandler(handler)

	// start informers
	factory.Start(ctx.Done())

	// wait for sync in background
	go func() {
		if ok := cache.WaitForCacheSync(ctx.Done(), crdInformer.HasSynced); !ok {
			if c.logger != nil {
				c.logger("CRD informer failed to sync")
			}
			return
		}
		if c.logger != nil {
			c.logger("CRD informer synced")
		}
	}()

	return nil
}
