# Montserrat ExtraBold

`Montserrat-ExtraBold.ttf` and `Montserrat-ExtraBoldItalic.ttf` are the official static TTFs from the upstream [JulietaUla/Montserrat](https://github.com/JulietaUla/Montserrat) repository.

- Upstream paths: `fonts/ttf/Montserrat-ExtraBold.ttf`, `fonts/ttf/Montserrat-ExtraBoldItalic.ttf`
- Tag: `v7.222`
- Commit: `5dae7a4ef9c0bf9fe48dc54fd1076eefaa0a8c7e`
- SHA256 (upright): `1b364c3400bf7b1cc2c47a25dd0d3edd8331da451412aa5539080f78f8f70b63`
- SHA256 (italic): `0984784ee9883389e76bf2c7ceeeda848af26535ec111109fa92e18d448a4759`
- License: SIL Open Font License 1.1, reproduced in `OFL.txt` (covers the whole Montserrat family)

The Go package embeds both files and materializes them into the same cache directory for FFmpeg and libass.
Both faces share the OpenType family name `Montserrat ExtraBold` (name ID 1); the upright face carries subfamily `Regular` and the italic carries subfamily `Italic`.
libass therefore resolves the italic from the shared family with the Italic flag set, so an ASS style asks for the italic via `Italic: -1`, not a separate family name.
