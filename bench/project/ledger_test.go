package ledger

import (
	"strings"
	"testing"
	"time"
)

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]Money{
		"12.34": 1234, "-5": -500, "€7.50": 750, "$0.5": 50, "1,234.00": 123400,
	} {
		got, err := ParseAmount(in)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseAmount("1.234"); err == nil {
		t.Error("three decimals: no error")
	}
}

func TestMoneyString(t *testing.T) {
	for m, want := range map[Money]string{1234: "12.34", -500: "-5.00", 7: "0.07"} {
		if got := m.String(); got != want {
			t.Errorf("%d.String() = %q; want %q", int64(m), got, want)
		}
	}
}

func TestSplitEvenly(t *testing.T) {
	shares, err := Split(900, 3)
	if err != nil || len(shares) != 3 || shares[0] != 300 {
		t.Fatalf("Split(900, 3) = %v, %v", shares, err)
	}
	if _, err := Split(100, 0); err != ErrNoPeople {
		t.Errorf("Split(100, 0): %v", err)
	}
}

func TestBalance(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	txs := []Transaction{{Date: day, Amount: 1000}, {Date: day, Amount: -250}}
	if got := Balance(txs); got != 750 {
		t.Errorf("Balance = %v", got)
	}
}

func TestParseCSV(t *testing.T) {
	txs, err := ParseCSV(strings.NewReader("date,description,category,amount\n2026-01-05,Rent,home,-950.00\n"))
	if err != nil || len(txs) != 1 || txs[0].Description != "Rent" || txs[0].Amount != -95000 {
		t.Fatalf("ParseCSV = %+v, %v", txs, err)
	}
}

func TestRemaining(t *testing.T) {
	txs := []Transaction{{Category: "food", Amount: -3000}, {Category: "home", Amount: -9000}}
	if got := Remaining(Budget{"food", 10000}, txs); got != 7000 {
		t.Errorf("Remaining = %v", got)
	}
}

func TestNextDate(t *testing.T) {
	d := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	if got, _ := NextDate(d, "weekly", 10); !got.Equal(d.AddDate(0, 0, 7)) {
		t.Errorf("weekly: %v", got)
	}
	if got, _ := NextDate(d, "monthly", 10); got.Month() != time.February || got.Day() != 10 {
		t.Errorf("monthly: %v", got)
	}
}
