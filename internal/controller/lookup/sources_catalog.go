// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rediscloud "github.com/RedisLabs/rediscloud-go-api"
	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/redis_rules"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/roles"
	"github.com/RedisLabs/rediscloud-go-api/service/access_control_lists/users"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/internal/lookup"
)

// ACLRoleReconciler looks up one ACL role.
type ACLRoleReconciler struct{ client.Client }

// SetupACLRole adds the ACL role lookup controller.
func SetupACLRole(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aclrole", &lookupv1alpha1.ACLRole{}, &ACLRoleReconciler{Client: mgr.GetClient()})
}

// Reconcile reads ACL roles and writes the single named match.
func (r *ACLRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ACLRole{}, req, aclRoleRef, aclRoleStatus, aclRoleAt, readACLRole)
}

func aclRoleRef(obj *lookupv1alpha1.ACLRole) xpv1.Reference { return obj.Spec.ProviderConfigRef }
func aclRoleStatus(obj *lookupv1alpha1.ACLRole) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aclRoleAt(obj *lookupv1alpha1.ACLRole) *lookupv1alpha1.ACLRoleObservation {
	return &obj.Status.AtProvider
}

func readACLRole(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ACLRole) (lookupv1alpha1.ACLRoleObservation, error) {
	items, err := api.Roles.List(ctx)
	if err != nil {
		return lookupv1alpha1.ACLRoleObservation{}, errors.Wrap(err, "cannot list ACL roles")
	}
	matched, err := lookup.MatchNamed(items, obj.Spec.ForProvider.Name, func(role *roles.GetRoleResponse) string {
		return redis.StringValue(role.Name)
	})
	if err != nil {
		return lookupv1alpha1.ACLRoleObservation{}, err
	}
	if err := requireACLIdentity(matched.ID); err != nil {
		return lookupv1alpha1.ACLRoleObservation{}, err
	}
	return lookup.ObserveACLRole(matched), nil
}

// ACLRuleReconciler looks up one ACL rule.
type ACLRuleReconciler struct{ client.Client }

// SetupACLRule adds the ACL rule lookup controller.
func SetupACLRule(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-aclrule", &lookupv1alpha1.ACLRule{}, &ACLRuleReconciler{Client: mgr.GetClient()})
}

// Reconcile reads ACL rules and writes the single named match.
func (r *ACLRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ACLRule{}, req, aclRuleRef, aclRuleStatus, aclRuleAt, readACLRule)
}

func aclRuleRef(obj *lookupv1alpha1.ACLRule) xpv1.Reference { return obj.Spec.ProviderConfigRef }
func aclRuleStatus(obj *lookupv1alpha1.ACLRule) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aclRuleAt(obj *lookupv1alpha1.ACLRule) *lookupv1alpha1.ACLRuleObservation {
	return &obj.Status.AtProvider
}

func readACLRule(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ACLRule) (lookupv1alpha1.ACLRuleObservation, error) {
	items, err := api.RedisRules.List(ctx)
	if err != nil {
		return lookupv1alpha1.ACLRuleObservation{}, errors.Wrap(err, "cannot list ACL rules")
	}
	matched, err := lookup.MatchNamed(items, obj.Spec.ForProvider.Name, func(rule *redis_rules.GetRedisRuleResponse) string {
		return redis.StringValue(rule.Name)
	})
	if err != nil {
		return lookupv1alpha1.ACLRuleObservation{}, err
	}
	if err := requireACLIdentity(matched.ID); err != nil {
		return lookupv1alpha1.ACLRuleObservation{}, err
	}
	return lookup.ObserveACLRule(matched), nil
}

// ACLUserReconciler looks up one ACL user.
type ACLUserReconciler struct{ client.Client }

// SetupACLUser adds the ACL user lookup controller.
func SetupACLUser(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-acluser", &lookupv1alpha1.ACLUser{}, &ACLUserReconciler{Client: mgr.GetClient()})
}

// Reconcile reads ACL users and writes the single named match.
func (r *ACLUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.ACLUser{}, req, aclUserRef, aclUserStatus, aclUserAt, readACLUser)
}

func aclUserRef(obj *lookupv1alpha1.ACLUser) xpv1.Reference { return obj.Spec.ProviderConfigRef }
func aclUserStatus(obj *lookupv1alpha1.ACLUser) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func aclUserAt(obj *lookupv1alpha1.ACLUser) *lookupv1alpha1.ACLUserObservation {
	return &obj.Status.AtProvider
}

func readACLUser(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.ACLUser) (lookupv1alpha1.ACLUserObservation, error) {
	items, err := api.Users.List(ctx)
	if err != nil {
		return lookupv1alpha1.ACLUserObservation{}, errors.Wrap(err, "cannot list ACL users")
	}
	matched, err := lookup.MatchNamed(items, obj.Spec.ForProvider.Name, func(user *users.GetUserResponse) string {
		return redis.StringValue(user.Name)
	})
	if err != nil {
		return lookupv1alpha1.ACLUserObservation{}, err
	}
	if err := requireACLIdentity(matched.ID); err != nil {
		return lookupv1alpha1.ACLUserObservation{}, err
	}
	return lookup.ObserveACLUser(matched), nil
}

func requireACLIdentity(id *int) error {
	if id == nil || *id <= 0 {
		return errors.New("ACL lookup response has no valid identity")
	}
	return nil
}

// CloudAccountReconciler looks up one cloud account.
type CloudAccountReconciler struct{ client.Client }

// SetupCloudAccount adds the cloud account lookup controller.
func SetupCloudAccount(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-cloudaccount", &lookupv1alpha1.CloudAccount{}, &CloudAccountReconciler{Client: mgr.GetClient()})
}

// Reconcile reads cloud accounts and writes the single match.
func (r *CloudAccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.CloudAccount{}, req, cloudAccountRef, cloudAccountStatus, cloudAccountAt, readCloudAccount)
}

func cloudAccountRef(obj *lookupv1alpha1.CloudAccount) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func cloudAccountStatus(obj *lookupv1alpha1.CloudAccount) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func cloudAccountAt(obj *lookupv1alpha1.CloudAccount) *lookupv1alpha1.CloudAccountObservation {
	return &obj.Status.AtProvider
}

func readCloudAccount(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.CloudAccount) (lookupv1alpha1.CloudAccountObservation, error) {
	items, err := api.CloudAccount.List(ctx)
	if err != nil {
		return lookupv1alpha1.CloudAccountObservation{}, errors.Wrap(err, "cannot list cloud accounts")
	}
	query := lookup.CloudAccountQuery{
		ProviderType: deref(obj.Spec.ForProvider.ProviderType),
		Name:         deref(obj.Spec.ForProvider.Name),
	}
	if obj.Spec.ForProvider.ExcludeInternalAccount != nil && *obj.Spec.ForProvider.ExcludeInternalAccount {
		query.ExcludeInternal = true
	}
	matched, err := lookup.MatchCloudAccount(items, query)
	if err != nil {
		return lookupv1alpha1.CloudAccountObservation{}, err
	}
	return lookup.ObserveCloudAccount(matched), nil
}

// DataPersistenceReconciler looks up the data persistence catalog.
type DataPersistenceReconciler struct{ client.Client }

// SetupDataPersistence adds the data persistence lookup controller.
func SetupDataPersistence(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-datapersistence", &lookupv1alpha1.DataPersistence{}, &DataPersistenceReconciler{Client: mgr.GetClient()})
}

// Reconcile reads the data persistence catalog.
func (r *DataPersistenceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.DataPersistence{}, req, dataPersistenceRef, dataPersistenceStatus, dataPersistenceAt, readDataPersistence)
}

func dataPersistenceRef(obj *lookupv1alpha1.DataPersistence) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func dataPersistenceStatus(obj *lookupv1alpha1.DataPersistence) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func dataPersistenceAt(obj *lookupv1alpha1.DataPersistence) *lookupv1alpha1.DataPersistenceObservation {
	return &obj.Status.AtProvider
}

func readDataPersistence(ctx context.Context, api *rediscloud.Client, _ *lookupv1alpha1.DataPersistence) (lookupv1alpha1.DataPersistenceObservation, error) {
	items, err := api.Account.ListDataPersistence(ctx)
	if err != nil {
		return lookupv1alpha1.DataPersistenceObservation{}, errors.Wrap(err, "cannot list data persistence options")
	}
	return lookup.ObserveDataPersistence(items), nil
}

// DatabaseModulesReconciler looks up the database module catalog.
type DatabaseModulesReconciler struct{ client.Client }

// SetupDatabaseModules adds the database module lookup controller.
func SetupDatabaseModules(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-databasemodules", &lookupv1alpha1.DatabaseModules{}, &DatabaseModulesReconciler{Client: mgr.GetClient()})
}

// Reconcile reads the database module catalog.
func (r *DatabaseModulesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.DatabaseModules{}, req, databaseModulesRef, databaseModulesStatus, databaseModulesAt, readDatabaseModules)
}

func databaseModulesRef(obj *lookupv1alpha1.DatabaseModules) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func databaseModulesStatus(obj *lookupv1alpha1.DatabaseModules) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func databaseModulesAt(obj *lookupv1alpha1.DatabaseModules) *lookupv1alpha1.DatabaseModulesObservation {
	return &obj.Status.AtProvider
}

func readDatabaseModules(ctx context.Context, api *rediscloud.Client, _ *lookupv1alpha1.DatabaseModules) (lookupv1alpha1.DatabaseModulesObservation, error) {
	items, err := api.Account.ListDatabaseModules(ctx)
	if err != nil {
		return lookupv1alpha1.DatabaseModulesObservation{}, errors.Wrap(err, "cannot list database modules")
	}
	return lookup.ObserveDatabaseModules(items), nil
}

// EssentialsSubscriptionReconciler looks up one Essentials subscription.
type EssentialsSubscriptionReconciler struct{ client.Client }

// SetupEssentialsSubscription adds the Essentials subscription lookup controller.
func SetupEssentialsSubscription(mgr ctrl.Manager) error {
	return setup(mgr, "lookup-essentialssubscription", &lookupv1alpha1.EssentialsSubscription{}, &EssentialsSubscriptionReconciler{Client: mgr.GetClient()})
}

// Reconcile reads Essentials subscriptions and writes the single match.
func (r *EssentialsSubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runLookup(ctx, r.Client, &lookupv1alpha1.EssentialsSubscription{}, req, essentialsSubscriptionRef, essentialsSubscriptionStatus, essentialsSubscriptionAt, readEssentialsSubscription)
}

func essentialsSubscriptionRef(obj *lookupv1alpha1.EssentialsSubscription) xpv1.Reference {
	return obj.Spec.ProviderConfigRef
}
func essentialsSubscriptionStatus(obj *lookupv1alpha1.EssentialsSubscription) *xpv1.ManagedResourceStatus {
	return &obj.Status.ManagedResourceStatus
}
func essentialsSubscriptionAt(obj *lookupv1alpha1.EssentialsSubscription) *lookupv1alpha1.EssentialsSubscriptionObservation {
	return &obj.Status.AtProvider
}

func readEssentialsSubscription(ctx context.Context, api *rediscloud.Client, obj *lookupv1alpha1.EssentialsSubscription) (lookupv1alpha1.EssentialsSubscriptionObservation, error) {
	items, err := api.FixedSubscriptions.List(ctx)
	if err != nil {
		return lookupv1alpha1.EssentialsSubscriptionObservation{}, errors.Wrap(err, "cannot list Essentials subscriptions")
	}
	matched, err := lookup.MatchEssentialsSubscription(items, lookup.EssentialsSubscriptionQuery{
		ID:   deref(obj.Spec.ForProvider.ID),
		Name: deref(obj.Spec.ForProvider.Name),
	})
	if err != nil {
		return lookupv1alpha1.EssentialsSubscriptionObservation{}, err
	}
	return lookup.ObserveEssentialsSubscription(matched), nil
}

func setup(mgr ctrl.Manager, name string, obj client.Object, reconciler reconcile.Reconciler) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(obj).
		Complete(reconciler)
}
