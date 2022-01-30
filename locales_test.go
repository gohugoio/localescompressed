package localescompressed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bep/workers"
	qt "github.com/frankban/quicktest"
	"github.com/gohugoio/locales"
	"github.com/gohugoio/locales/currency"
	"github.com/gohugoio/locales/en"
	"github.com/gohugoio/locales/nn"
)

func TestGetTranslator(t *testing.T) {
	c := qt.New(t)

	d, _ := time.Parse("2006-Jan-02", "2018-Jan-06")

	assertSame := func(c *qt.C, tr1, tr2 locales.Translator) {
		num := 120.842
		precision := uint64(2)
		curr := currency.USD
		d := time.Now()

		c.Assert(tr1.CardinalPluralRule(num, precision), qt.Equals, tr2.CardinalPluralRule(num, precision))
		c.Assert(tr1.FmtNumber(num, precision), qt.Equals, tr2.FmtNumber(num, precision))
		c.Assert(tr1.FmtPercent(num, precision), qt.Equals, tr2.FmtPercent(num, precision))

		c.Assert(tr1.FmtAccounting(num, precision, curr), qt.Equals, tr2.FmtAccounting(num, precision, curr))
		c.Assert(tr1.FmtCurrency(num, precision, curr), qt.Equals, tr2.FmtCurrency(num, precision, curr))

		c.Assert(tr1.FmtDateFull(d), qt.Equals, tr2.FmtDateFull(d))
		c.Assert(tr1.FmtDateLong(d), qt.Equals, tr2.FmtDateLong(d))
		c.Assert(tr1.FmtDateMedium(d), qt.Equals, tr2.FmtDateMedium(d))
		c.Assert(tr1.FmtDateShort(d), qt.Equals, tr2.FmtDateShort(d))

		c.Assert(tr1.FmtTimeFull(d), qt.Equals, tr2.FmtTimeFull(d))
		c.Assert(tr1.FmtTimeLong(d), qt.Equals, tr2.FmtTimeLong(d))
		c.Assert(tr1.FmtTimeMedium(d), qt.Equals, tr2.FmtTimeMedium(d))
		c.Assert(tr1.FmtTimeShort(d), qt.Equals, tr2.FmtTimeShort(d))

		c.Assert(tr1.WeekdayAbbreviated(d.Weekday()), qt.Equals, tr2.WeekdayAbbreviated(d.Weekday()))
		c.Assert(tr1.MonthAbbreviated(d.Month()), qt.Equals, tr2.MonthAbbreviated(d.Month()))
	}

	c.Run("Basic", func(c *qt.C) {
		tnn := GetTranslator("nn_NO")
		c.Assert(tnn, qt.Not(qt.IsNil))
		c.Assert(tnn.MonthWide(d.Month()), qt.Equals, "januar")
	})

	c.Run("Basic", func(c *qt.C) {
		tnn := GetTranslator("nn-NO")
		c.Assert(tnn, qt.Not(qt.IsNil))
		c.Assert(tnn.MonthWide(d.Month()), qt.Equals, "januar")
	})

	// Sample tests; verify that the compression script works correctly.
	c.Run("Sample", func(c *qt.C) {
		assertSame(c, GetTranslator("en"), en.New())
		assertSame(c, GetTranslator("nn"), nn.New())
		// find . -name "*.go" | xargs grep "New() locales.Translator" | wc -l in locales.
		c.Assert(translatorFuncs, qt.HasLen, 764)
	})

	c.Run("Para", func(c *qt.C) {
		p := workers.New(4)
		r, _ := p.Start(context.Background())

		for i := 0; i < 10; i++ {
			for _, locale := range []string{"nn_NO", "nn", "nyn", "sg", "se", "rwk", "mas"} {
				locale := locale
				r.Run(func() error {
					tnn := GetTranslator(locale)
					if tnn == nil {
						return errors.New("translator is nil")
					}

					if tnn.MonthWide(d.Month()) == "" {
						return errors.New("translator is invalid")
					}

					return nil
				})
			}
		}
	})
}

func TestGetCurrency(t *testing.T) {
	c := qt.New(t)
	c.Assert(GetCurrency("NOK"), qt.Equals, currency.NOK)
	c.Assert(GetCurrency("USD"), qt.Equals, currency.USD)
	c.Assert(GetCurrency("usd"), qt.Equals, currency.USD)
	c.Assert(GetCurrency("foo"), qt.Equals, currency.Type(-1))
}
