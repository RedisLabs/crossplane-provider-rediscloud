// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"errors"
	"testing"
	"time"

	"github.com/RedisLabs/rediscloud-go-api/service/account"
)

func TestMatchPaymentMethod(t *testing.T) {
	now := time.Date(2026, time.October, 23, 0, 0, 0, 0, time.UTC)
	visa := method(10, "Visa", 4242, 12, 2027)
	expired := method(11, "Visa", 1111, 1, 2020)
	mc := method(12, "Mastercard", 9999, 12, 2027)

	got, err := MatchPaymentMethod([]*account.PaymentMethod{visa, expired, mc}, PaymentMethodQuery{
		CardType:       "Visa",
		ExcludeExpired: true,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if redisID(got) != 10 {
		t.Fatalf("id = %d, want 10", redisID(got))
	}
	if LastFour(got) != "4242" {
		t.Fatalf("last four = %s", LastFour(got))
	}

	_, err = MatchPaymentMethod([]*account.PaymentMethod{visa, mc}, PaymentMethodQuery{ExcludeExpired: true, Now: now})
	if !errors.Is(err, ErrMany) {
		t.Fatalf("many: %v", err)
	}

	_, err = MatchPaymentMethod([]*account.PaymentMethod{expired}, PaymentMethodQuery{ExcludeExpired: true, Now: now})
	if !errors.Is(err, ErrNone) {
		t.Fatalf("none: %v", err)
	}

	got, err = MatchPaymentMethod([]*account.PaymentMethod{visa}, PaymentMethodQuery{
		LastFourNumbers: "4242",
		ExcludeExpired:  false,
		Now:             now,
	})
	if err != nil || redisID(got) != 10 {
		t.Fatalf("last four match: %v %#v", err, got)
	}
}

func method(id int, brand string, lastFour, month, year int) *account.PaymentMethod {
	return &account.PaymentMethod{
		ID:                 &id,
		Type:               &brand,
		CreditCardEndsWith: &lastFour,
		ExpirationMonth:    &month,
		ExpirationYear:     &year,
	}
}

func redisID(method *account.PaymentMethod) int {
	if method == nil || method.ID == nil {
		return 0
	}
	return *method.ID
}
