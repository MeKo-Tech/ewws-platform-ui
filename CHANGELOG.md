# Changelog

## 1.0.0 (2026-10-29)


### ⚠ BREAKING CHANGES

* **claim:** the claim form no longer produces `secrets`; app secrets go through sealed_secrets in the registry PR.

### Features

* add admin-only usage view with ewws-auth login activity ([#32](https://github.com/MeKo-Tech/ewws-platform-ui/issues/32)) ([3defef7](https://github.com/MeKo-Tech/ewws-platform-ui/commit/3defef746ef06027c38d666e84293bff1a11a793))
* **bootstrap:** per-repo secrets convention (Phase 1) ([#14](https://github.com/MeKo-Tech/ewws-platform-ui/issues/14)) ([6f5290a](https://github.com/MeKo-Tech/ewws-platform-ui/commit/6f5290a502d1b7b9d24e8cd4e6db5d8ebffbdc67))
* **claim:** bootstrap source repo on claim (Phase 2c write-side) ([#5](https://github.com/MeKo-Tech/ewws-platform-ui/issues/5)) ([5803fd6](https://github.com/MeKo-Tech/ewws-platform-ui/commit/5803fd681fb1a0df5e9869c31d6c527344d28f53))
* **claim:** keep the image tag only in tags/&lt;slug&gt;/&lt;stage&gt;.yaml ([#36](https://github.com/MeKo-Tech/ewws-platform-ui/issues/36)) ([9821112](https://github.com/MeKo-Tech/ewws-platform-ui/commit/982111240ba0f782249b3bac0bdfbc82c543d35f))
* **claim:** offer images, stages, env, secrets, auth and features in the form ([#28](https://github.com/MeKo-Tech/ewws-platform-ui/issues/28)) ([1fb8324](https://github.com/MeKo-Tech/ewws-platform-ui/commit/1fb8324968fb57a0d3682e42db3e75f603d65382))
* **claim:** remove ExternalSecrets claim field ([#31](https://github.com/MeKo-Tech/ewws-platform-ui/issues/31)) ([d7f9e57](https://github.com/MeKo-Tech/ewws-platform-ui/commit/d7f9e577db4d5b39a2792f24814cddb3b68516b7))
* **compliance:** periodic scanner + dashboard compliance column ([#3](https://github.com/MeKo-Tech/ewws-platform-ui/issues/3)) ([49ed22b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/49ed22b11ff6c09eabb1bef60231ac51f022ca9f))
* **dashboard:** traffic + drift + activity overview, cards + table views ([#6](https://github.com/MeKo-Tech/ewws-platform-ui/issues/6)) ([6768791](https://github.com/MeKo-Tech/ewws-platform-ui/commit/676879183bd110ec826e2a7cad665fbbdf255495))
* **detail:** per-stage 7d/30d traffic chart + drift via SHA prefix ([#7](https://github.com/MeKo-Tech/ewws-platform-ui/issues/7)) ([556293b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/556293b7808d6e18cd32cab8b40b483954d00265))
* **detail:** retro-bootstrap button on /app/&lt;slug&gt; ([#13](https://github.com/MeKo-Tech/ewws-platform-ui/issues/13)) ([4735f35](https://github.com/MeKo-Tech/ewws-platform-ui/commit/4735f3572485f2d7e4d0d13f001334d4ab3c575c))
* **docs:** /docs onboarding + best-practice page ([#12](https://github.com/MeKo-Tech/ewws-platform-ui/issues/12)) ([dbb1582](https://github.com/MeKo-Tech/ewws-platform-ui/commit/dbb1582300a46dc85bb69ed5c53ee5ddc096478e))
* GITHUB_API_TOKEN for reading the private registry; argo-cd token optional ([#2](https://github.com/MeKo-Tech/ewws-platform-ui/issues/2)) ([9d1532c](https://github.com/MeKo-Tech/ewws-platform-ui/commit/9d1532c2410983ded5ae9db7f7284ec8b6f0eebd))
* initial scaffold for the vibe-tenant platform UI ([765e0e8](https://github.com/MeKo-Tech/ewws-platform-ui/commit/765e0e87874b5470ac5edb0ba765ed8c73e8d490))
* **repobootstrap:** use reusable workflows in claim templates ([#10](https://github.com/MeKo-Tech/ewws-platform-ui/issues/10)) ([6729beb](https://github.com/MeKo-Tech/ewws-platform-ui/commit/6729beb3eb05d63a414be235d472c81675ae4825))
* **ui:** add Neue App call-to-action and update route table ([#26](https://github.com/MeKo-Tech/ewws-platform-ui/issues/26)) ([aead126](https://github.com/MeKo-Tech/ewws-platform-ui/commit/aead126a165184b053b98d1c9b336f0aab05dcc4))
* **ui:** link the onboarding guide from the header ([#25](https://github.com/MeKo-Tech/ewws-platform-ui/issues/25)) ([a99eb96](https://github.com/MeKo-Tech/ewws-platform-ui/commit/a99eb96dc6a2c8a405f65b9e15d0565ce8f265df))
* **ui:** show permissions and URLs of an app's ewws-auth scope ([#24](https://github.com/MeKo-Tech/ewws-platform-ui/issues/24)) ([d019e20](https://github.com/MeKo-Tech/ewws-platform-ui/commit/d019e2020548aab838ecfcd22aef3fc84512cbf9))
* **ui:** show repo link and ewws-auth scope on overview and detail ([#23](https://github.com/MeKo-Tech/ewws-platform-ui/issues/23)) ([e89f779](https://github.com/MeKo-Tech/ewws-platform-ui/commit/e89f779585a166b691eb318d00f6914df0394f9f))


### Bug Fixes

* **ci:** bump Go toolchain to 1.25.x to match go.mod ([#4](https://github.com/MeKo-Tech/ewws-platform-ui/issues/4)) ([2bbee9d](https://github.com/MeKo-Tech/ewws-platform-ui/commit/2bbee9dc54a37d01f2a2ca1ad71b29d5669d7d3e))
* **ci:** make golangci-lint green on main ([e30640a](https://github.com/MeKo-Tech/ewws-platform-ui/commit/e30640aa25d74e2227b6b280419d945c337ebd26))
* **ci:** make golangci-lint green on main ([923e796](https://github.com/MeKo-Tech/ewws-platform-ui/commit/923e79612a12e60dc99e3f882d01a9e0a236c3d9))
* **claim:** drop obsolete enabled:false rollout hint ([#29](https://github.com/MeKo-Tech/ewws-platform-ui/issues/29)) ([e6393a6](https://github.com/MeKo-Tech/ewws-platform-ui/commit/e6393a61cd3ef67b44d3e84cdbeab48911285e7f))
* **claim:** render claims that pass the registry's validate-apps ([#27](https://github.com/MeKo-Tech/ewws-platform-ui/issues/27)) ([1e4f2d4](https://github.com/MeKo-Tech/ewws-platform-ui/commit/1e4f2d4e1f615373dea4915b4dc43a6c691f166e))
* **deps:** bump the github-actions group with 3 updates ([596d21f](https://github.com/MeKo-Tech/ewws-platform-ui/commit/596d21f966392859253cb5022d47fbc969362169))
* **deps:** bump the github-actions group with 3 updates ([4149f58](https://github.com/MeKo-Tech/ewws-platform-ui/commit/4149f588835ec0ab82156e7e64850ae58dda276e))
* **deps:** bump the go group with 2 updates ([9d293f2](https://github.com/MeKo-Tech/ewws-platform-ui/commit/9d293f284638fcff931339293a820ad83bd27886))
* **deps:** bump the go group with 2 updates ([f545035](https://github.com/MeKo-Tech/ewws-platform-ui/commit/f545035ccf35f20d9ef324ce1ab6af74b5f420f2))
* **deps:** update Go 1.27, dependencies and base images ([#21](https://github.com/MeKo-Tech/ewws-platform-ui/issues/21)) ([88b7ccb](https://github.com/MeKo-Tech/ewws-platform-ui/commit/88b7ccb99e1aa058f604dcd59f2c9515e0e7da3e))
* **lint:** compact long http.Error to satisfy wsl_v5 ([7768edf](https://github.com/MeKo-Tech/ewws-platform-ui/commit/7768edf4ee9792281ee068e8bed0377c98d6a4e9))
* **lint:** inline string concat in auth.go to fit wsl_v5 branch-max-lines ([aeb4b0b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/aeb4b0b89cd4c6528421d4a7a87995c94a0f06f5))
* **repobootstrap:** point sops template at current prod/staging key versions ([#22](https://github.com/MeKo-Tech/ewws-platform-ui/issues/22)) ([bb0b719](https://github.com/MeKo-Tech/ewws-platform-ui/commit/bb0b719dff51c13964f7264250d48a9e83c56098))
