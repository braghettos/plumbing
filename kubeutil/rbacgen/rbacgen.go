// Package rbacgen builds least-privilege Role/RoleBinding objects that grant a single ServiceAccount
// access to an exact, named set of resources (e.g. "these Secrets, by name, and nothing else"). It is
// new code modeled on the Role/RoleBinding shape already proven in production (per-secret,
// resourceNames-scoped grants for chart credentials), not a port of any existing Go implementation: no
// prior pure-Go builder produced a ServiceAccount-subject, resourceNames-scoped Role anywhere in the
// krateo codebases this was modeled on — that shape previously existed only as a Helm/Sprig template.
//
// The returned objects are meant to be applied via a dynamic client (e.g. kubeutil/objectclient), which
// requires their GroupVersionKind to be set explicitly — a plain rbacv1 struct literal leaves TypeMeta
// empty, which makes a dynamic-client apply fail. BuildRole and BuildRoleBinding both set TypeMeta.
package rbacgen

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// BuildRole returns a namespace-scoped Role named roleName in namespace ns, granting verbs on the given
// apiGroup/resource, restricted via resourceNames to exactly resourceNames (never a whole resource type).
func BuildRole(ns, roleName, apiGroup, resource string, resourceNames []string, verbs []string) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{apiGroup},
				Resources:     []string{resource},
				Verbs:         verbs,
				ResourceNames: resourceNames,
			},
		},
	}
}

// BuildRoleForSecret is BuildRole specialized for core/v1 Secrets, the shape #31's secretRef resolver
// and oasgen-provider's auth-secret RBAC both need.
func BuildRoleForSecret(ns, roleName string, secretNames []string, verbs []string) *rbacv1.Role {
	return BuildRole(ns, roleName, "", "secrets", secretNames, verbs)
}

// BuildRoleBinding returns a RoleBinding named bindingName in namespace ns, binding a single
// ServiceAccount subject (which may live in a different namespace than the Role, e.g. the controller's
// own namespace binding into a workload's namespace) to the Role named roleName.
func BuildRoleBinding(ns, bindingName, roleName string, sa types.NamespacedName) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindingName,
			Namespace: ns,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      sa.Name,
				Namespace: sa.Namespace,
			},
		},
	}
}

// BuildRoleBindingForRole is an alias for BuildRoleBinding kept for call-site clarity at secretRef/
// auth-secret RBAC call sites, which always bind exactly one Role to exactly one ServiceAccount.
func BuildRoleBindingForRole(ns, bindingName, roleName string, sa types.NamespacedName) *rbacv1.RoleBinding {
	return BuildRoleBinding(ns, bindingName, roleName, sa)
}
