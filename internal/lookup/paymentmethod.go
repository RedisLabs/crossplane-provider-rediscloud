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
		match, err := paymentMethodMatches(method, query)
		if err != nil {
			return nil, err
		}
		if match {
			matched = append(matched, method)
		}
	}
	return selectedPaymentMethod(matched)
}

// Unknown selector inputs are only material if no known filter excludes the
// row. This avoids falsely reporting one unique candidate from a partial list.
func paymentMethodMatches(method *account.PaymentMethod, query PaymentMethodQuery) (bool, error) { //nolint:gocyclo // Independent selector decisions must preserve definite exclusions.
	if method == nil {
		return false, fmt.Errorf("payment method entry is missing")
	}
	unknown := false
	if query.CardType != "" {
		if method.Type == nil {
			unknown = true
		} else if *method.Type != query.CardType {
			return false, nil
		}
	}
	if query.LastFourNumbers != "" {
		if method.CreditCardEndsWith == nil {
			unknown = true
		} else if LastFour(method) != query.LastFourNumbers {
			return false, nil
		}
	}
	if query.ExcludeExpired {
		if method.ExpirationYear == nil {
			unknown = true
		} else if *method.ExpirationYear < query.Now.Year() {
			return false, nil
		} else if *method.ExpirationYear == query.Now.Year() {
			if method.ExpirationMonth == nil {
				unknown = true
			} else if *method.ExpirationMonth < int(query.Now.Month()) {
				return false, nil
			}
		}
	}
	if unknown {
		return false, fmt.Errorf("payment method filter information is missing")
	}
	return true, nil
}

func selectedPaymentMethod(matched []*account.PaymentMethod) (*account.PaymentMethod, error) {
	selected, err := SelectOne(matched)
	if err != nil {
		return nil, err
	}
	if selected.ID == nil {
		return nil, fmt.Errorf("payment method id is missing")
	}
	return selected, nil
}

// LastFour formats the card ending as four digits, matching the Terraform provider.
func LastFour(method *account.PaymentMethod) string {
	if method == nil || method.CreditCardEndsWith == nil {
		return ""
	}
	return fmt.Sprintf("%04d", redis.IntValue(method.CreditCardEndsWith))
}
