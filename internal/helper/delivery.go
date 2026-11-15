package helper

// Typed delivery (reliability-council 02): what a failure says about its
// Action reaching the app travels as a value, from the native error
// through the helper's *Error to the Response (protocol.Stamp takes it
// before the text's phrases). The text is unchanged: the phrases ("nothing
// was sent", "may have taken effect", "was delivered") stay the agent's,
// and the fallback for a site that sets no delivery.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// mayHaveActed marks a failure whose Action may have taken effect: a
// pattern write that failed, which the provider may have carried out
// before failing.
func mayHaveActed(e *Error) *Error {
	e.Delivery = protocol.DeliveryUncertain
	return e
}

// wasDelivered marks a failure whose Action reached the app but whose
// read-back did not match: do not repeat it.
func wasDelivered(e *Error) *Error {
	e.Delivery = protocol.DeliveryDeliveredUnverified
	return e
}

// nativeDelivery is what a native failure proves about its Action: a
// failed pattern write may have taken effect (uia.WriteError), a failed
// read-back was delivered (uia.VerifyError), and a refusal raised before
// any pattern call sent nothing (refusedBeforeCall). "" when the error
// does not say.
func nativeDelivery(err error) string {
	var write *uia.WriteError
	var verify *uia.VerifyError
	switch {
	case errors.As(err, &verify):
		return protocol.DeliveryDeliveredUnverified
	case errors.As(err, &write):
		return protocol.DeliveryUncertain
	case errors.Is(err, uia.ErrNoClickRoute), errors.Is(err, uia.ErrRaiseHeadless), refusedBeforeCall(err):
		return protocol.DeliveryNotDelivered
	}
	return ""
}

// Delivery returns a failure's delivery: an *Error's own, else the native
// error's (nativeDelivery), else what its text says (protocol.DeliveryOf);
// "" when nothing says.
func Delivery(err error) string {
	var e *Error
	if errors.As(err, &e) {
		if e.Delivery != "" {
			return e.Delivery
		}
		d, _ := protocol.DeliveryOf(e.Msg, e.Hint)
		return d
	}
	if d := nativeDelivery(err); d != "" || err == nil {
		return d
	}
	d, _ := protocol.DeliveryOf(err.Error(), "")
	return d
}

// SentNothing reports whether a failure provably sent nothing to the app,
// so repeating its Action cannot act twice. A failure that may have taken
// effect, was delivered, or does not say, is not one.
func SentNothing(err error) bool { return protocol.SentNothing(Delivery(err)) }

// actionDelivered gives every failure of an Action a delivery: the site's
// own, or the text's, and otherwise uncertain. A failure that does not
// say whether it was sent may have been, so a retry is not safe. A usage
// error (exit 2) is never sent, and Stamp says so.
func actionDelivered(cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		out, err := cmd(ctx, args)
		if err != nil && err.Exit != 2 && Delivery(err) == "" {
			e := *err
			e.Delivery = protocol.DeliveryUncertain
			return out, &e
		}
		return out, err
	}
}
