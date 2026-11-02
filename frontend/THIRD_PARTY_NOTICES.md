OTONEI reused/adapted files include the React presentation components, full theme/layout
styles, assets listed below, and src/services/AudioEngine.ts (frontend/src/AudioEngine.ts). Source: https://github.com/voici5986/OTONEI.

MIT License

Copyright (c) 2026 voici5986

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.


## Original OTONEI presentation reuse

Source: https://github.com/voici5986/OTONEI (MIT, copyright (c) 2026 voici5986; license above).

- `frontend/src/OtoneiPresentation.tsx` adapts original `src/components/DesktopNavbar.jsx`, `MobileBottomNav.jsx`, `Header.jsx`, `SearchResultItem.tsx`, `DesktopPlayerView.jsx`, `DesktopPlayerControl.jsx`, `DesktopExpandedView.jsx`, `DesktopAlbumCover.tsx`, and `PlayerSubComponents.jsx` markup and class contracts. Unsupported account, download and cloud controls were omitted; all playback actions bind to OTONEIGO's existing PlayerStore.
- `frontend/src/App.tsx` adapts the original `src/App.jsx` shell; `LibraryPanels.tsx` adapts `SearchResultItem.tsx` and `HeartButton.jsx` presentation.
- `frontend/src/otonei/` retains original `src/index.css` and `src/styles/theme.css`, `primitives.css`, `App.css`, `Navigation.desktop.css`, `Navigation.mobile.css`, `Header.desktop.css`, `AudioPlayer.base.css`, `AudioPlayer.desktop.css`. Custom media aliases are expanded to the original 768px breakpoint. `frontend/src/styles.css` supplies local desktop adapters.
- `frontend/public/logo.svg`, `default_cover.svg` and `lyric.svg` are copied from original `public/`.

## MiSans font

`frontend/public/MiSansVF.woff2` is copied unchanged from OTONEI/public. MiSans is separately licensed, not covered by the OTONEI MIT license. Copyright belongs to Xiaomi and the relevant rights holders. The supplied agreement is retained at `frontend/public/licenses/MiSans-License.pdf`. Official source: https://hyperos.mi.com/font/ . The app identifies MiSans and its copyright in Settings.

## react-icons and bundled icons

react-icons 5.7.0: https://github.com/react-icons/react-icons (MIT, Copyright 2018 kamijin_fanta). Its complete package notice is retained in `frontend/public/licenses/react-icons-LICENSE.txt` and the packaged THIRD_PARTY_NOTICES.txt.

Font Awesome Free icons (`react-icons/fa`): copyright Fonticons, Inc.; icons licensed CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/), https://fontawesome.com/ . Icons are used without artwork modification.

Material Design icons (`react-icons/md`): copyright Google; Apache License 2.0 (https://www.apache.org/licenses/LICENSE-2.0), https://github.com/google/material-design-icons . Icons are used without artwork modification.

The full Apache License 2.0 text for Material Design icons is retained at `frontend/public/licenses/MaterialIcons-LICENSE.txt` and included in the packaged notices.
