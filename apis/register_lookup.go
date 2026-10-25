// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package apis

import lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"

func init() {
	AddToSchemes = append(AddToSchemes, lookupv1alpha1.AddToScheme)
}
