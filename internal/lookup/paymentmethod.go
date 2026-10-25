// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"fmt"
	"time"

	"github.com/RedisLabs/rediscloud-go-api/redis"
	"github.com/RedisLabs/rediscloud-go-api/service/account"
)

// PaymentMethodQuery is the filter for one payment method.
type PaymentMethodQuery struct {
	CardType        string
	LastFourNumbers string
	ExcludeExpired  bool
	Now             time.Time
}

// MatchPaymentMethod applies the Terraform rediscloud_payment_method filters
// and returns the single match.
func MatchPaymentMethod(methods []*account.PaymentMethod, query PaymentMethodQuery) (*account.PaymentMethod, error) {
	matched := make([]*account.PaymentMethod, 0, len(methods))
	for _, method := range methods {
		if method == nil {
			continue
		}
		if query.ExcludeExpired && expired(method, query.Now) {
			continue
		}
		if query.CardType != "" && redis.StringValue(method.Type) != query.CardType {
			continue
		}
		if query.LastFourNumbers != "" && LastFour(method) != query.LastFourNumbers {
			continue
		}
		matched = append(matched, method)
	}
	return SelectOne(matched)
}

// LastFour formats the card ending as four digits, matching the Terraform provider.
func LastFour(method *account.PaymentMethod) string {
	if method == nil {
		return ""
	}
	return fmt.Sprintf("%04d", redis.IntValue(method.CreditCardEndsWith))
}

func expired(method *account.PaymentMethod, now time.Time) bool {
	year := redis.IntValue(method.ExpirationYear)
	month := redis.IntValue(method.ExpirationMonth)
	if year < now.Year() {
		return true
	}
	if year > now.Year() {
		return false
	}
	return month < int(now.Month())
}
