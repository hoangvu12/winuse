# 02: Actions in a background Tab, Headless only

Status: done

Spec decision 3 and the native-popup half of 5 (ADR 0017 Decisions 2 and 4).

**Done.**

- `refuseOnTab`: commands with no Page route, `--headed`, `--post`, `@x,y`
  (click, scroll and hover points, drag to a point), the partial reads and
  the element waits are refused before anything is resolved or held; a
  Batch is checked step by step (no `in <window>`/`in new`).
- Keys and text on a Tab target always go to its Page (`pageKeys`), the
  body included; there is no address bar route to fall back to.
- `nativePickerRefusal` (`pageactions.go`) with `page.Origin.NativePicker`:
  a click on a `<select>` drop-down (or one of its options) or a date, time,
  colour, month or week input of a background Tab is refused with the
  `set-value` retry.
- `set-value` on a date/time/colour input (`choice.go`: `picker` kind,
  `Origin.SetPicker`): the value set from the page's JS with input and
  change events, read back, with the format named on a mismatch. This also
  applies to the active Tab, where set-value used to refuse these inputs.
- Tests: `TestTabTargetActsWithoutTouchingTheUsersTab` (click by index and
  name, double click, set-value, type-text plain and `--into`, press-key,
  scroll, hover, drag, eval, a Batch), `TestTabTargetNativePopups`,
  `TestTabTargetRefusals`.

## Comments
