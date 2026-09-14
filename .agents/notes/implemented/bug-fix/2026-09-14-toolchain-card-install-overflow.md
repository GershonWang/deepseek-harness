# Agent Note: Installing tool cards overflow their row in the toolchain market

Status: implemented

English | [中文](2026-09-14-toolchain-card-install-overflow.zh.md)

## Problem

While any market card is installing, its action row (the version dropdown and the install button) is painted outside the card border, on top of the cards in the row below. A user screenshot of the JDK install shows the measurements: the card box spans y 170–304 (about 141px), its action row y 291–317, and the next row's cards start at y 321 — the whole action row is outside the border, and the version dropdown then opens from that displaced position.

The installing state renders two rows more than a normal card (`el.append(head, desc, meta, progress, pctLabel, actions)` in `frontend/app.js`). WebKitGTK sizes an auto grid row from the item's minimum contribution, which for these cards is the `min-height` floor, so content beyond the floor overflows instead of growing the row. The existing answer to that is a `min-height` bucket per card shape (`.has-runtime` → 160px); the installing shape had none.

A second defect came out of the same screenshot: in the light theme the progress track paints `--bg-input` (#ffffff), the card's own background, so only the fill is visible.

## Decision

Three changes in `frontend/styles.css`:

1. `.market-grid` declares `grid-auto-rows: max-content`. Engines that honor it size the row to its content, and the `min-height` buckets degrade to a floor.
2. The installing shapes get their own buckets: `.tool-card-item.installing { min-height: 176px }` and `.tool-card-item.installing.has-runtime { min-height: 198px }`. The values take the content height measured by the preview gate in Chromium (172 / 194) as their floor plus 4px; the real-machine content height is about 160 / 182, so both engines fit.
3. `.tool-progress` paints `--bg-hover` (light #e0e0e0, dark #37373d) instead of `--bg-input`, so the track is visible against the card in both themes.

## Why both a bucket and max-content

Whether WebKitGTK honors `grid-auto-rows: max-content` cannot be verified here: the only local engine is Chromium, which grows the row anyway and so hides the difference. The buckets' effectiveness is verified on the real machine instead — the measured row height equals the `min-height` exactly, and `.has-runtime`'s 160px bucket exists for the same reason. One change is therefore the forward fix and the other the guarantee; they do not conflict, because the bucket is only a floor.

## Alternatives considered

**Only `grid-auto-rows: max-content`.** If WebKitGTK ignores the value the fix does nothing, which stakes the repair on an unverified implementation, so it is not used alone.

**Only the buckets.** That repairs today's shape, but the next card shape with an extra row repeats the defect; `max-content` removes the class of failures.

**Move the progress bar into the meta row and fold the percentage into the badge.** The card would stay at four rows and the grid would not jump while installing, but it changes the presentation in `app.js` and its copy, whose comment argues that one number need not appear three times. That is a design trade-off, not part of this fix.

**Replace the native `<select>` with a custom menu.** The native popup necessarily overlays the card below it on WebKitGTK; after this fix it still covers the top of the next row. A custom menu is a much larger change and was not taken.

## Consequences

The row holding an installing card becomes about 35px taller, and the other two cards in that row grow with it, leaving empty space at their bottoms. That follows from sharing one row height and is independent of how the height is derived; only removing the extra rows during install would avoid it.

The progress track is now visible in the light theme, which fixes the second defect found in the same screenshot.

The native dropdown still covers the top of the cards in the next row. Avoiding that entirely needs a custom menu, which was not taken.

## Testing

`node apps/desktop-launcher/frontend/tools/preview.mjs verify` gained two assertions: every card shape's `min-height` bucket must cover that shape's natural content height, and the installing shapes' progress track must not share the card's background color. Mutation checks: deleting the two installing buckets fails four assertions (`bucket 141/160 cannot hold content 172/194`), and restoring `--bg-input` for the track fails the light theme with an invisible track; restoring both makes the gate pass again.

Not verified: only Chromium exists locally, so neither WebKitGTK's row-height pinning nor whether it honors `max-content` was reproduced on the real engine. The buckets are floored by the real-machine content height (about 160 / 182) and set to the Chromium measurement plus 4px; confirming on the real machine needs a rebuilt launcher and a visual check.

## Related

- [Toolchain market presentation](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.md) owns the dialog's card layout and control styling.
- [Multi-version JDK entries in the toolchain market catalog](../feature/2026-09-14-toolchain-market-jdk-multiversion.md) is where the defect surfaced, because the new version dropdown made the overflowing row more visible; it did not introduce it.
