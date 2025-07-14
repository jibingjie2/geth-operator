/*
Copyright 2025.

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

package v1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	xttv1 "github.com/jibingjie/geth-operator/api/v1"
)

// nolint:unused
// log is for logging in this package.
var gethlog = logf.Log.WithName("geth-resource")

// SetupGethWebhookWithManager registers the webhook for Geth in the manager.
func SetupGethWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&xttv1.Geth{}).
		WithValidator(&GethCustomValidator{}).
		WithDefaulter(&GethCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-xtt-xyz-v1-geth,mutating=true,failurePolicy=fail,sideEffects=None,groups=xtt.xyz,resources=geths,verbs=create;update,versions=v1,name=mgeth-v1.kb.io,admissionReviewVersions=v1

// GethCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Geth when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type GethCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting

}

var _ webhook.CustomDefaulter = &GethCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Geth.
// 为geth设置默认值
func (d *GethCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	geth, ok := obj.(*xttv1.Geth)
	if geth.Spec.Password == "" {
		_ = geth.Spec.Password == "123456"
	}

	if !ok {
		return fmt.Errorf("expected an Geth object but got %T", obj)
	}
	gethlog.Info("Defaulting for Geth", "name", geth.GetName())

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-xtt-xyz-v1-geth,mutating=false,failurePolicy=fail,sideEffects=None,groups=xtt.xyz,resources=geths,verbs=create;update,versions=v1,name=vgeth-v1.kb.io,admissionReviewVersions=v1

// GethCustomValidator struct is responsible for validating the Geth resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type GethCustomValidator struct {

	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &GethCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Geth.
func (v *GethCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	geth, ok := obj.(*xttv1.Geth)
	if !ok {
		return nil, fmt.Errorf("expected a Geth object but got %T", obj)
	}
	gethlog.Info("Validation for Geth upon creation", "name", geth.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Geth.
func (v *GethCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldGeth, ok1 := oldObj.(*xttv1.Geth)
	newGeth, ok2 := newObj.(*xttv1.Geth)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("expected a Geth object for the newObj or oldObj but got \r\n oldObj: %T \r\n newObj:%T", oldObj, newObj)
	}
	gethlog.Info("Validation for Geth upon update", "name", newGeth.GetName())

	if oldGeth.Spec.Password != newGeth.Spec.Password {
		return nil, fmt.Errorf("密码不允许修改")
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Geth.
func (v *GethCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	geth, ok := obj.(*xttv1.Geth)
	if !ok {
		return nil, fmt.Errorf("expected a Geth object but got %T", obj)
	}
	gethlog.Info("Validation for Geth upon deletion", "name", geth.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
