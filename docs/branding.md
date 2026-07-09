# Branding

## Palette
- Indigo `#6D5EF6` → Teal `#19C4B4` (primary gradient, top-left → bottom-right)
- Indigo shade `#5A4CD8`, dark screen `#0E1230`, white `#FFFFFF`

## Assets
- `assets/logo.svg` — the mark: a padlock enclosing two linked nodes = *encrypted memory, shared between two agents*. Use for the repo icon, favicon, and social card.
- `assets/mascot.svg` — "Sync", a friendly bot mascot (v0, hand-authored). Fine as-is; the prompt below will produce a more polished raster version.

## Rendering to PNG
No renderer ships on macOS by default. Any of:
```sh
brew install librsvg && rsvg-convert -w 512 -h 512 assets/logo.svg > logo.png
# or
npx --yes svgexport assets/logo.svg logo.png 512:512
```

## Image-gen prompt for a polished mascot
> A friendly, rounded robot mascot named "Sync" for a developer tool. Soft
> squircle body with a smooth indigo-to-teal gradient (#6D5EF6 → #19C4B4), a
> dark rounded face screen with two big glowing white eyes and a small cheerful
> smile, a short antenna with a glowing teal tip, and a small circular
> "sync/refresh" swirl glowing on its chest. It gently holds a small glowing
> padlock orb representing shared, encrypted memory. Clean flat vector style,
> thick soft shapes, subtle depth, centered, on a transparent background. Modern,
> approachable, tech-brand mascot. No text.

Square (1:1). Ask for a transparent PNG plus a monochrome variant for small sizes.
