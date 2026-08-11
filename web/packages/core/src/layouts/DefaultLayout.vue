// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * DefaultLayout - Main layout component.
 *
 * Sidebar + header (collapse toggle + breadcrumb + theme switch + locale switch
 * + user dropdown) + content area.
 *
 * Architecture (aligned with docs/frontend/navigation-design.md §6):
 * - Base menus are injected via provide/inject (BASE_MENUS_KEY) to avoid
 *   circular dependency between core and features. App root provides baseMenus.
 * - extension injects extra menus via `extraMenus` prop.
 * - Feature-flag filtering via filterMenusByFeature, controlled by useUserStore.features.
 * - Theme and sidebar collapse state managed by useAppStore, persisted to localStorage.
 * - Theme toggle cycles: light → dark → auto → light.
 * - Locale dropdown renders from availableLocales registry; extension locales
 *   registered via registerLocale appear automatically.
 */
import { computed, inject, onBeforeUnmount, onMounted, ref, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '../stores/app'
import { useUserStore } from '../stores/user'
import { availableLocales } from '../i18n'
import { BASE_MENUS_KEY } from '../symbols'
import { filterMenusByFeature } from '../composables/useMenuFilter'
import LogoMark from '../components/LogoMark.vue'
import type { MenuItem, MenuBadge } from '../types/menu'
import type { ThemeMode } from '../types/global'

/**
 * Layout component Props
 */
interface Props {
  /** Extension-injected extra menu items, appended after base menus */
  extraMenus?: MenuItem[]
}

const props = withDefaults(defineProps<Props>(), {
  extraMenus: () => [],
})

// Topbar icons (used directly as components, not via dynamic resolution)
import IFold from '~icons/ep/fold'
import IExpand from '~icons/ep/expand'
import IArrowDown from '~icons/ep/arrow-down'
import IArrowRight from '~icons/ep/arrow-right'
import IMoon from '~icons/ep/moon'
import ISunny from '~icons/ep/sunny'
import IMonitor from '~icons/ep/monitor'
import ICheck from '~icons/ep/check'
import IUser from '~icons/ep/user'
import IKey from '~icons/ep/key'
import ISwitchButton from '~icons/ep/switch-button'
import IMenu from '~icons/ep/menu'

/**
 * Resolve menu icon name to a renderable component name.
 *
 * Supports two naming conventions:
 * - Element Plus PascalCase names (e.g. `Odometer`, `Calendar`) — resolved directly
 *   via globally registered components (registered in app main.ts)
 * - unplugin-icons `i-ep-kebab-case` names (e.g. `i-ep-data-board`) — converted
 *   to PascalCase Element Plus component name for backward compatibility
 */
function resolveIconName(icon?: string): string | undefined {
  if (!icon) return undefined
  // Convert i-ep-kebab-case to PascalCase Element Plus component name
  if (icon.startsWith('i-ep-')) {
    const kebab = icon.slice(5) // strip 'i-ep-' prefix
    return kebab
      .split('-')
      .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
      .join('')
  }
  return icon
}

/** Breadcrumb item */
interface BreadcrumbItem {
  title: string
  path: string
}

const route = useRoute()
const router = useRouter()
const { t, te, locale } = useI18n()
const appStore = useAppStore()
const userStore = useUserStore()

const sidebarCollapsed = computed(() => appStore.sidebar.collapsed)
// Theme state is applied to documentElement via the store's data-theme attribute;
// CSS variables switch automatically based on [data-theme="dark"], so components
// don't need to read an isDark flag.
// To check the effective theme in JS, use appStore.effectiveTheme (not the theme preference),
// so auto mode correctly reflects the system-resolved light/dark.
const activeMenu = computed(() => route.path)
const username = computed(() => userStore.username || 'admin')

/** First letter of the username for the avatar */
const avatarText = computed(() => username.value.charAt(0).toUpperCase())

/**
 * Sidebar width (reactively bound to CSS variables).
 * Expanded uses --tk-sidebar-width; collapsed uses --tk-sidebar-collapsed-width.
 */
const sidebarWidth = computed(() =>
  sidebarCollapsed.value
    ? 'var(--tk-sidebar-collapsed-width)'
    : 'var(--tk-sidebar-width)',
)

// ===== Mobile sidebar (drawer) =====
// Below 960px the static aside is hidden and a hamburger button reveals the
// navigation inside an el-drawer. This keeps the layout usable on phones and
// small tablets where a permanent sidebar would consume too much width.
const MOBILE_BREAKPOINT = 960
const isMobileViewport = ref(false)
const mobileSidebarOpen = ref(false)

function syncMobileViewport(): void {
  if (typeof window === 'undefined') return
  isMobileViewport.value = window.innerWidth < MOBILE_BREAKPOINT
  // Closing the drawer when leaving mobile avoids a stale open state.
  if (!isMobileViewport.value) {
    mobileSidebarOpen.value = false
  }
}

function openMobileSidebar(): void {
  mobileSidebarOpen.value = true
}

function closeMobileSidebar(): void {
  mobileSidebarOpen.value = false
}

/** Menu select handler wrapper that also closes the mobile drawer. */
function handleMobileMenuSelect(path: string): void {
  handleMenuSelect(path)
  closeMobileSidebar()
}

onMounted(() => {
  syncMobileViewport()
  window.addEventListener('resize', syncMobileViewport)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', syncMobileViewport)
  }
})

/**
 * i18n translation with fallback.
 *
 * When the i18n key is not yet defined in the locale pack, returns the fallback text
 * for the current language, avoiding raw key strings in the UI. Once the features layer
 * fills in the key, the translation switches to the formal one automatically.
 */
function tf(key: string, zhFallback: string, enFallback: string): string {
  if (te(key)) return t(key)
  return locale.value === 'en-US' ? enFallback : zhFallback
}

/** Theme preference → icon component map (light/dark/auto tri-state) */
const themeIconMap: Record<ThemeMode, Component> = {
  light: ISunny,
  dark: IMoon,
  auto: IMonitor,
}

/** Theme preference → i18n key map (for button tooltip) */
const themeTooltipKeyMap: Record<ThemeMode, string> = {
  light: 'common.theme.light',
  dark: 'common.theme.dark',
  auto: 'common.theme.auto',
}

/** Icon component for the current theme preference (reflects user choice, not effective theme) */
const currentThemeIcon = computed<Component>(
  () => themeIconMap[appStore.theme],
)

/** Tooltip text for the current theme preference */
const themeTooltip = computed(() =>
  tf(
    themeTooltipKeyMap[appStore.theme],
    appStore.theme === 'light'
      ? '明亮模式'
      : appStore.theme === 'dark'
        ? '暗黑模式'
        : '跟随系统',
    appStore.theme === 'light'
      ? 'Light Mode'
      : appStore.theme === 'dark'
        ? 'Dark Mode'
        : 'Follow System',
  ),
)

/** Display label of the current locale (looked up from availableLocales registry, falls back to code) */
const currentLocaleLabel = computed(() => {
  const found = availableLocales.value.find((item) => item.code === appStore.locale)
  return found?.label ?? appStore.locale
})

/** Tooltip text for the locale switcher */
const languageTooltip = computed(() =>
  tf('common.language.switch', '切换语言', 'Switch Language'),
)

/** Profile label text */
const profileLabel = computed(() =>
  tf('auth.user.profile', '个人资料', 'Profile'),
)

/** Injected base menus from app root (avoid circular core↔features dependency) */
const injectedBaseMenus = inject(BASE_MENUS_KEY, [])

/** Granted feature flag identifiers (keys with true value) for menu filtering */
const userFeatures = computed(() => {
  const flags = userStore.features ?? {}
  return Object.keys(flags).filter((key) => flags[key])
})

/**
 * All menus after merging base + extra, then filtering by feature flags.
 *
 * Aligned with docs/frontend/navigation-design.md §6:
 * - Base menus (from features via provide/inject) + extra menus (extension prop)
 * - filterMenusByFeature removes items without granted feature flags
 * - hidden items (detail/edit pages) are excluded from sidebar
 */
const allMenus = computed(() => {
  const merged = [...injectedBaseMenus, ...(props.extraMenus ?? [])]
  return filterMenusByFeature(merged, userFeatures.value)
})

/**
 * Derive breadcrumbs from the current route and menu tree.
 *
 * Matching logic: level-1 module > level-2 sub-module (if matched).
 * Leaf menus return a single-level breadcrumb; parent menus return two levels.
 *
 * Parent (level-1) breadcrumb items link to their first accessible child rather
 * than the parent path itself: parent paths are layout-only routes with no
 * index page, so navigating to them renders a blank router-view. Linking to
 * the first child follows breadcrumb best practices (every ancestor item must
 * resolve to a real, navigable page).
 */
const breadcrumbs = computed<BreadcrumbItem[]>(() => {
  const path = route.path
  if (!path || path === '/') return []

  for (const menu of allMenus.value) {
    if (menu.children?.length) {
      const child = menu.children.find(
        (c) => path === c.path || path.startsWith(c.path + '/'),
      )
      if (child) {
        // Parent links to its first accessible child (parent path has no page)
        const parentPath = menu.children[0]?.path ?? menu.path
        return [
          { title: t(menu.title), path: parentPath },
          { title: t(child.title), path: child.path },
        ]
      }
    } else if (path === menu.path || path.startsWith(menu.path + '/')) {
      return [{ title: t(menu.title), path: menu.path }]
    }
  }
  return []
})

/** Menu select navigation, supports external links */
function handleMenuSelect(path: string) {
  // Find the menu item to check for externalLink
  const item = findMenuItemByPath(allMenus.value, path)
  if (item?.externalLink) {
    window.open(item.externalLink, '_blank')
    return
  }
  router.push(path)
}

/** Recursively find a menu item by path */
function findMenuItemByPath(menus: MenuItem[], path: string): MenuItem | undefined {
  for (const menu of menus) {
    if (menu.path === path) return menu
    if (menu.children) {
      const found = findMenuItemByPath(menu.children, path)
      if (found) return found
    }
  }
  return undefined
}

/** Format badge display value (99+ for counts > 99) */
function formatBadgeValue(badge: MenuBadge): string | number {
  if (badge.type === 'count' && typeof badge.value === 'number') {
    return badge.value > 99 ? '99+' : badge.value
  }
  return badge.value ?? ''
}

/** Tri-state theme cycle: light → dark → auto → light */
function toggleTheme() {
  const order: ThemeMode[] = ['light', 'dark', 'auto']
  const idx = order.indexOf(appStore.theme)
  const next = order[(idx + 1) % order.length] as ThemeMode
  appStore.setTheme(next)
}

/** Locale dropdown switch */
function handleLocaleChange(code: string) {
  appStore.setLocale(code)
}

/** User dropdown command handler */
function handleUserCommand(command: string) {
  switch (command) {
    case 'profile':
      router.push('/system/settings/general')
      break
    case 'password':
      router.push('/change-password')
      break
    case 'logout':
      void handleLogout()
      break
  }
}

/**
 * Logout.
 *
 * The core layout only clears local state; the logout API call is orchestrated by
 * the features layer (if server-side logout is needed).
 * Local cleanup is enough to return the frontend to the unauthenticated state;
 * the route guard will intercept subsequent access.
 */
async function handleLogout() {
  userStore.clearUser()
  router.push('/login')
}
</script>

<template>
  <el-container class="tk-default-layout">
    <!-- Skip link: keyboard accessibility (WCAG 2.4.1 Level A) -->
    <a
      href="#main-content"
      class="tk-skip-link"
    >
      {{ tf('common.skipToMainContent', '跳到主内容', 'Skip to main content') }}
    </a>
    <!-- Sidebar -->
    <el-aside
      :width="sidebarWidth"
      class="tk-default-layout__sidebar"
      :class="{ 'tk-default-layout__sidebar--mobile-hidden': isMobileViewport }"
    >
      <div
        class="tk-default-layout__logo"
        :class="{ 'tk-default-layout__logo--collapsed': sidebarCollapsed }"
      >
        <slot name="sidebar-header">
          <LogoMark :size="32" />
          <h1 class="tk-default-layout__title">
            {{ t('common.app.title') }}
          </h1>
        </slot>
      </div>
      <el-menu
        :default-active="activeMenu"
        :collapse="sidebarCollapsed"
        :collapse-transition="false"
        unique-opened
        popper-effect="light"
        popper-class="tk-menu-popper"
        class="tk-default-layout__menu"
        role="navigation"
        @select="handleMenuSelect"
      >
        <template
          v-for="menu in allMenus"
          :key="menu.path"
        >
          <el-sub-menu
            v-if="menu.children"
            :index="menu.path"
          >
            <template #title>
              <el-icon v-if="resolveIconName(menu.icon)">
                <component :is="resolveIconName(menu.icon)" />
              </el-icon>
              <span>{{ t(menu.title) }}</span>
              <el-badge
                v-if="menu.badge"
                :type="menu.badge.color || 'danger'"
                :value="menu.badge.type === 'count' ? formatBadgeValue(menu.badge) : undefined"
                :is-dot="menu.badge.type === 'dot'"
                :class="['tk-sidebar-badge', { 'tk-sidebar-badge--animated': menu.badge.isAnimated }]"
              >
                <span
                  v-if="menu.badge.type === 'text'"
                  class="tk-sidebar-badge__text"
                >{{ formatBadgeValue(menu.badge) }}</span>
              </el-badge>
            </template>
            <el-menu-item
              v-for="child in menu.children"
              :key="child.path"
              :index="child.path"
            >
              <span>{{ t(child.title) }}</span>
              <el-badge
                v-if="child.badge"
                :type="child.badge.color || 'danger'"
                :value="child.badge.type === 'count' ? formatBadgeValue(child.badge) : undefined"
                :is-dot="child.badge.type === 'dot'"
                :class="['tk-sidebar-badge', { 'tk-sidebar-badge--animated': child.badge.isAnimated }]"
              >
                <span
                  v-if="child.badge.type === 'text'"
                  class="tk-sidebar-badge__text"
                >{{ formatBadgeValue(child.badge) }}</span>
              </el-badge>
            </el-menu-item>
          </el-sub-menu>
          <el-menu-item
            v-else
            :index="menu.path"
          >
            <el-icon v-if="resolveIconName(menu.icon)">
              <component :is="resolveIconName(menu.icon)" />
            </el-icon>
            <template #title>
              <span>{{ t(menu.title) }}</span>
              <el-badge
                v-if="menu.badge"
                :type="menu.badge.color || 'danger'"
                :value="menu.badge.type === 'count' ? formatBadgeValue(menu.badge) : undefined"
                :is-dot="menu.badge.type === 'dot'"
                :class="['tk-sidebar-badge', { 'tk-sidebar-badge--animated': menu.badge.isAnimated }]"
              >
                <span
                  v-if="menu.badge.type === 'text'"
                  class="tk-sidebar-badge__text"
                >{{ formatBadgeValue(menu.badge) }}</span>
              </el-badge>
            </template>
          </el-menu-item>
        </template>
      </el-menu>
      <div class="tk-default-layout__sidebar-footer">
        <slot name="sidebar-footer" />
      </div>
    </el-aside>

    <el-container class="tk-default-layout__main">
      <!-- Header -->
      <el-header class="tk-default-layout__header">
        <div class="tk-default-layout__header-left">
          <slot name="header-left">
            <!-- Hamburger button (mobile only, opens the drawer sidebar) -->
            <button
              v-if="isMobileViewport"
              class="tk-default-layout__collapse-btn tk-default-layout__hamburger-btn"
              type="button"
              aria-haspopup="dialog"
              :aria-expanded="mobileSidebarOpen"
              :aria-label="tf('common.layout.sidebar.openMenu', '打开菜单', 'Open menu')"
              @click="openMobileSidebar"
            >
              <el-icon>
                <IMenu />
              </el-icon>
            </button>
            <!-- Collapse button (desktop only) -->
            <button
              v-if="!isMobileViewport"
              class="tk-default-layout__collapse-btn"
              type="button"
              :aria-label="sidebarCollapsed ? tf('common.layout.sidebar.expand', '展开侧边栏', 'Expand sidebar') : tf('common.layout.sidebar.collapse', '收起侧边栏', 'Collapse sidebar')"
              @click="appStore.toggleSidebar"
            >
              <el-icon>
                <IFold v-if="!sidebarCollapsed" />
                <IExpand v-else />
              </el-icon>
            </button>
            <!-- Breadcrumb -->
            <el-breadcrumb
              v-if="breadcrumbs.length"
              :separator-icon="IArrowRight"
              class="tk-default-layout__breadcrumb"
            >
              <el-breadcrumb-item
                v-for="(item, idx) in breadcrumbs"
                :key="item.path"
                :to="idx < breadcrumbs.length - 1 ? item.path : undefined"
              >
                {{ item.title }}
              </el-breadcrumb-item>
            </el-breadcrumb>
          </slot>
        </div>
        <!-- Extension injection point: extra header content (e.g. tenant switcher)
             placed before the default header-right actions so extensions can add
             header elements without replacing the theme/locale/user dropdowns. -->
        <slot name="header-extra" />
        <div class="tk-default-layout__header-right">
          <slot name="header-right">
            <!-- Theme toggle icon button (tri-state cycle: light → dark → auto → light) -->
            <el-tooltip
              :content="themeTooltip"
              placement="bottom"
            >
              <button
                class="tk-default-layout__icon-btn"
                type="button"
                :aria-label="themeTooltip"
                @click="toggleTheme"
              >
                <el-icon>
                  <component :is="currentThemeIcon" />
                </el-icon>
              </button>
            </el-tooltip>
            <!-- Locale switcher dropdown -->
            <el-tooltip
              :content="languageTooltip"
              placement="bottom"
            >
              <el-dropdown
                trigger="click"
                popper-class="tk-lang-popper"
                @command="handleLocaleChange"
              >
                <div class="tk-default-layout__lang">
                  <span class="tk-default-layout__lang-label">{{ currentLocaleLabel }}</span>
                  <el-icon class="tk-default-layout__arrow">
                    <IArrowDown />
                  </el-icon>
                </div>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      v-for="loc in availableLocales"
                      :key="loc.code"
                      :command="loc.code"
                    >
                      <span
                        class="tk-default-layout__lang-option"
                        :class="{ 'tk-default-layout__lang-option--active': loc.code === appStore.locale }"
                      >
                        {{ loc.label }}
                      </span>
                      <el-icon
                        v-if="loc.code === appStore.locale"
                        class="tk-default-layout__lang-check"
                      >
                        <ICheck />
                      </el-icon>
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </el-tooltip>
            <!-- User dropdown menu -->
            <el-dropdown
              trigger="click"
              @command="handleUserCommand"
            >
              <div class="tk-default-layout__user">
                <el-avatar
                  :size="32"
                  class="tk-default-layout__avatar"
                >
                  {{ avatarText }}
                </el-avatar>
                <span class="tk-default-layout__username">{{ username }}</span>
                <el-icon class="tk-default-layout__arrow">
                  <IArrowDown />
                </el-icon>
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    command="profile"
                    :icon="IUser"
                  >
                    {{ profileLabel }}
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="password"
                    :icon="IKey"
                  >
                    {{ t('auth.login.changePassword') }}
                  </el-dropdown-item>
                  <el-dropdown-item
                    divided
                    command="logout"
                    :icon="ISwitchButton"
                  >
                    {{ t('auth.login.logout') }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </slot>
        </div>
      </el-header>

      <!-- Content area -->
      <el-main
        id="main-content"
        class="tk-default-layout__content"
        tabindex="-1"
      >
        <slot>
          <router-view />
        </slot>
      </el-main>
    </el-container>

    <!-- Mobile navigation drawer (shown below 960px) -->
    <el-drawer
      v-model="mobileSidebarOpen"
      :title="t('common.app.title')"
      direction="ltr"
      size="280px"
      :with-header="false"
      class="tk-default-layout__mobile-drawer"
      @close="closeMobileSidebar"
    >
      <div class="tk-default-layout__mobile-drawer-body">
        <div class="tk-default-layout__logo tk-default-layout__logo--mobile">
      <LogoMark :size="32" />
      <h1 class="tk-default-layout__title">
        {{ t('common.app.title') }}
      </h1>
    </div>
        <el-menu
          :default-active="activeMenu"
          :collapse="false"
          :collapse-transition="false"
          popper-effect="light"
          class="tk-default-layout__menu tk-default-layout__menu--mobile"
          role="navigation"
          @select="handleMobileMenuSelect"
        >
          <template
            v-for="menu in allMenus"
            :key="menu.path"
          >
            <el-sub-menu
              v-if="menu.children"
              :index="menu.path"
            >
              <template #title>
                <el-icon v-if="resolveIconName(menu.icon)">
                  <component :is="resolveIconName(menu.icon)" />
                </el-icon>
                <span>{{ t(menu.title) }}</span>
              </template>
              <el-menu-item
                v-for="child in menu.children"
                :key="child.path"
                :index="child.path"
              >
                <span>{{ t(child.title) }}</span>
              </el-menu-item>
            </el-sub-menu>
            <el-menu-item
              v-else
              :index="menu.path"
            >
              <el-icon v-if="resolveIconName(menu.icon)">
                <component :is="resolveIconName(menu.icon)" />
              </el-icon>
              <template #title>
                <span>{{ t(menu.title) }}</span>
              </template>
            </el-menu-item>
          </template>
        </el-menu>
      </div>
    </el-drawer>
  </el-container>
</template>

<style scoped lang="scss">
.tk-default-layout {
  height: 100vh;

  // Menu item horizontal padding (overrides Element Plus default 20px).
  // Exposed at the layout root so both the static sidebar and the mobile
  // drawer can reference the same value, keeping the logo left edge
  // aligned with the first-level menu icon left edge in both surfaces.
  --tk-menu-item-padding-x: var(--tk-spacing-lg);

  // Sub-menu (level-2) text indent: equals the parent menu's icon column
  // (24px icon + 5px right margin) so nested item text aligns exactly under
  // the parent item's text, giving a clear hierarchy without an extra icon.
  --tk-menu-sub-indent: 29px;

  // Menu icon render size (Element Plus default). Used to compute the
  // collapsed-state icon centering so icons slide into place (instead of
  // jumping) in sync with the sidebar width transition.
  --tk-menu-icon-size: 24px;

  // ===== Sidebar =====
  &__sidebar {
    display: flex;
    flex-shrink: 0;
    flex-direction: column;
    overflow: hidden;
    background-color: var(--tk-bg-color-sidebar);
    transition: width var(--tk-transition-slow);
  }

  &__logo {
    display: flex;
    flex-shrink: 0;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: flex-start;
    height: var(--tk-header-height);

    // Align the logo's left edge with the first-level menu icon left edge:
    // menu padding (sm) + menu-item padding (tk-menu-item-padding-x)
    padding: 0 var(--tk-spacing-md) 0 calc(var(--tk-spacing-sm) + var(--tk-menu-item-padding-x));
    color: var(--tk-text-sidebar-active);

    // Slide the logo mark to its centered position in sync with the sidebar
    // width transition. justify-content stays flex-start (not flipped to
    // center) because it is not animatable; the mark's horizontal position
    // is driven by padding-left, which transitions smoothly.
    transition: padding var(--tk-transition-slow);

    // Collapsed: center the mark within the narrowed sidebar via padding-left
    // = (sidebar width - mark width) / 2. The mark (32px) size must match the
    // LogoMark :size prop. padding-left animates from the expanded value so
    // the mark slides instead of jumping to center.
    &--collapsed {
      padding-left: calc((var(--tk-sidebar-collapsed-width) - 32px) / 2);
    }
  }

  &__title {
    max-width: 200px;
    margin: 0;
    overflow: hidden;
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-sidebar-active);
    letter-spacing: -0.02em;
    white-space: nowrap;
    opacity: 1;

    // Fade + collapse the title in sync with the sidebar width transition.
    // The title is always rendered (no v-if) so CSS can transition it;
    // otherwise v-if would remove it instantly and produce a jarring cut.
    transition: opacity var(--tk-transition-slow),
      max-width var(--tk-transition-slow);

    .tk-default-layout__logo--collapsed & {
      max-width: 0;
      opacity: 0;
    }
  }

  // Sidebar menu: override el-menu dark theme via :deep
  &__menu {
    flex: 1;
    padding: var(--tk-spacing-sm);
    overflow: hidden auto;
    border-right: none;

    :deep(.el-menu) {
      background-color: var(--tk-bg-color-sidebar);
      border-right: none;
    }

    :deep(.el-menu-item),
    :deep(.el-sub-menu__title) {
      height: 40px;
      margin: 2px 0;
      font-size: var(--tk-font-size-sm);
      font-weight: var(--tk-font-weight-medium);
      line-height: 40px;
      color: var(--tk-text-sidebar);
      background-color: transparent;
      border-radius: var(--tk-border-radius-md);

      // padding transitions slowly so icons slide into their centered
      // collapsed position in sync with the sidebar width transition
      // (background/color stay fast for snappy hover feedback).
      transition: padding var(--tk-transition-slow),
        background-color var(--tk-transition-fast),
        color var(--tk-transition-fast);

      &:hover {
        color: var(--tk-text-sidebar-hover);
        background-color: var(--tk-bg-sidebar-hover);
      }
    }

    // Expanded: override Element Plus default 20px padding to align menu
    // icons with the logo (logo padding-left = menu padding + this padding).
    &:not(.el-menu--collapse) {
      :deep(.el-menu-item),
      :deep(.el-sub-menu__title) {
        padding-right: var(--tk-menu-item-padding-x);
        padding-left: var(--tk-menu-item-padding-x);
      }

      // Nested sub-menu items: indent so their text aligns under the parent
      // item's text (parent icon column), establishing a clear hierarchy.
      :deep(.el-menu--inline .el-menu-item),
      :deep(.el-menu--inline .el-sub-menu__title) {
        padding-left: calc(var(--tk-menu-item-padding-x) + var(--tk-menu-sub-indent));
      }
    }

    // Collapsed: Element Plus fixes the menu width to 64px (4px narrower than
    // the 68px sidebar) and gives items padding 0 20px with justify-content
    // normal, which leaves icons off-center. Stretch the menu to the full
    // sidebar width and center the icons so they sit on the sidebar's vertical
    // axis (matching the logo mark). Centering is done via padding-left (not
    // justify-content: center) because justify-content is not animatable;
    // padding-left transitions, so the icon slides into place in sync with the
    // sidebar width transition instead of jumping. The menu's 8px horizontal
    // padding is kept so hover/active pills stay inset, consistent with the
    // expanded state.
    &.el-menu--collapse {
      width: 100%;

      :deep(.el-menu-item),
      :deep(.el-sub-menu__title) {
        justify-content: flex-start;
        padding: 0;
        padding-left: calc(
          (var(--tk-sidebar-collapsed-width) - var(--tk-menu-icon-size)) / 2 -
            var(--tk-spacing-sm)
        );
      }

      // Element Plus wraps collapsed item content in a tooltip trigger that
      // keeps padding 0 20px and flex-start; reset it to the same
      // padding-based centering so the icon sits on the sidebar axis.
      :deep(.el-menu-tooltip__trigger) {
        justify-content: flex-start;
        padding: 0;
        padding-left: calc(
          (var(--tk-sidebar-collapsed-width) - var(--tk-menu-icon-size)) / 2 -
            var(--tk-spacing-sm)
        );
      }
    }

    :deep(.el-menu-item.is-active) {
      font-weight: var(--tk-font-weight-semibold);
      color: var(--tk-text-sidebar-active);
      background-color: var(--tk-bg-sidebar-active);
    }

    :deep(.el-sub-menu.is-active > .el-sub-menu__title) {
      color: var(--tk-text-sidebar-active);
    }

    // Nested sub-menu container. Element Plus animates this element's
    // max-height via el-collapse-transition, which toggles `overflow: hidden`
    // during the animation. With block layout the items' vertical margins
    // collapse through the container while overflow is visible but are
    // contained while it is hidden, so the height measured at the start of
    // the animation differs from the resting height and the menu visibly
    // shifts ("jitters") when the transition ends. A flex column with `gap`
    // removes margin collapsing entirely — the height is identical before,
    // during and after the transition. Item vertical margins are zeroed
    // (spacing comes from the gap) to avoid double spacing.
    :deep(.el-menu--inline) {
      display: flex;
      flex-direction: column;
      gap: var(--tk-spacing-1);
    }

    :deep(.el-menu--inline .el-menu-item),
    :deep(.el-menu--inline .el-sub-menu__title) {
      margin-top: 0;
      margin-bottom: 0;
    }

    // Prevent Element Plus' fixed min-width on nested items from forcing
    // horizontal overflow inside the (narrower) inline sub-menu container.
    :deep(.el-menu--inline .el-menu-item) {
      min-width: auto;
    }
  }

  &__sidebar-footer {
    flex-shrink: 0;
    padding: var(--tk-spacing-sm);
    margin-top: auto;
  }

  // ===== Main area =====
  &__main {
    flex-direction: column;
    min-width: 0;
    overflow: hidden;
  }

  // ===== Header =====
  &__header {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: space-between;
    height: var(--tk-header-height);
    padding: 0 var(--tk-spacing-xl);
    background-color: var(--tk-bg-color);
    border-bottom: 1px solid var(--tk-border-color);
  }

  &__header-left,
  &__header-right {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
  }

  // Collapse button (rounded icon button)
  &__collapse-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    padding: 0;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background-color: transparent;
    border: none;
    border-radius: var(--tk-border-radius-md);
    transition: background-color var(--tk-transition-fast),
      color var(--tk-transition-fast);

    &:hover {
      color: var(--tk-text-primary);
      background-color: var(--tk-bg-hover);
    }

    .el-icon {
      font-size: 20px;
    }
  }

  // Generic icon button (theme toggle etc.)
  &__icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    padding: 0;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background-color: transparent;
    border: none;
    border-radius: var(--tk-border-radius-base);
    transition: background-color var(--tk-transition-fast),
      color var(--tk-transition-fast);

    &:hover {
      color: var(--tk-text-primary);
      background-color: var(--tk-gray-3);
    }

    .el-icon {
      font-size: 20px;
    }
  }

  // ===== Locale switcher area =====
  &__lang {
    display: flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    height: 40px;
    padding: 0 var(--tk-spacing-sm);
    color: var(--tk-text-secondary);
    cursor: pointer;
    background-color: transparent;
    border: none;
    border-radius: var(--tk-border-radius-base);
    transition: background-color var(--tk-transition-fast),
      color var(--tk-transition-fast);

    &:hover {
      color: var(--tk-text-primary);
      background-color: var(--tk-gray-3);
    }
  }

  &__lang-label {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    white-space: nowrap;
  }

  &__lang-option {
    flex: 1;

    &--active {
      font-weight: var(--tk-font-weight-semibold);
      color: var(--tk-text-link);
    }
  }

  &__lang-check {
    margin-left: var(--tk-spacing-sm);
    font-size: 14px;
    color: var(--tk-text-link);
  }

  // ===== Breadcrumb =====
  &__breadcrumb {
    :deep(.el-breadcrumb__item) {
      .el-breadcrumb__inner {
        font-size: var(--tk-font-size-sm);
        font-weight: var(--tk-font-weight-normal);
        color: var(--tk-text-secondary);
        transition: color var(--tk-transition-fast);
      }

      .el-breadcrumb__inner.is-link:hover {
        color: var(--tk-text-primary);
      }

      // Active state: last item uses primary color + medium weight
      &:last-child .el-breadcrumb__inner {
        font-weight: var(--tk-font-weight-medium);
        color: var(--tk-text-link);
      }
    }

    :deep(.el-breadcrumb__separator) {
      font-size: var(--tk-font-size-sm);
      color: var(--tk-text-tertiary);
    }
  }

  // ===== User area =====
  &__user {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
    cursor: pointer;
    border-radius: var(--tk-border-radius-md);
    transition: background-color var(--tk-transition-fast);

    &:hover {
      background-color: var(--tk-bg-hover);
    }
  }

  &__avatar {
    flex-shrink: 0;
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-on-primary);
    background: var(--tk-gradient-brand-accent);
  }

  &__username {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-primary);
    white-space: nowrap;
  }

  &__arrow {
    font-size: 12px;
    color: var(--tk-text-tertiary);
  }

  // ===== Content area =====
  &__content {
    padding: var(--tk-spacing-2xl);
    overflow-y: auto;
    background-color: var(--tk-bg-color-page);
  }
}

// ===== Menu badge =====
.tk-sidebar-badge {
  margin-left: var(--tk-spacing-sm);
  vertical-align: middle;

  &__text {
    display: inline-block;
    padding: 0 6px;
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    line-height: 16px;
    color: var(--tk-text-on-primary);
    background-color: var(--tk-primary-color);
    border-radius: 10px;
  }

  &--animated {
    animation: tk-badge-pulse 1.5s ease-in-out infinite;
  }
}

@keyframes tk-badge-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }

  50% {
    opacity: 0.7;
    transform: scale(1.1);
  }
}

// Respect reduced-motion preference (docs §10.5)
@media (prefers-reduced-motion: reduce) {
  .tk-sidebar-badge--animated {
    animation: none !important;
  }
}

// ===== Mobile sidebar visibility =====
// The static aside is hidden below 960px; navigation moves into the drawer.
.tk-default-layout__sidebar--mobile-hidden {
  display: none !important;
}

// Hamburger button inherits collapse-btn sizing; no extra rules needed,
// it only renders when isMobileViewport is true.

// ===== Mobile responsive =====
@media (max-width: 767px) {
  .tk-default-layout {
    &__header {
      padding: 0 var(--tk-spacing-md);
    }

    &__username {
      display: none;
    }

    &__content {
      padding: var(--tk-spacing-md);
    }
  }
}

// ===== Mobile drawer body =====
.tk-default-layout__mobile-drawer-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  background-color: var(--tk-bg-color-sidebar);
}

.tk-default-layout__logo--mobile {
  justify-content: flex-start;

  // Match the expanded sidebar logo alignment (menu padding + menu-item padding)
  padding: 0 var(--tk-spacing-md) 0 calc(var(--tk-spacing-sm) + var(--tk-spacing-lg));
}
</style>

<!--
  Non-scoped global styles: only for el-menu collapsed state teleported to body.
  scoped styles cannot target elements under body, so global class names are required.
  All class names carry the tk- prefix, no global pollution.
-->
<style lang="scss">
/* Collapsed sub-menu popper + leaf menu tooltip shared popper container */
.tk-menu-popper.el-popper {
  border-radius: var(--tk-border-radius-md);
}

/* List inside sub-menu popper (only effective when .el-menu exists, does not affect leaf tooltip) */
.tk-menu-popper .el-menu {
  padding: var(--tk-spacing-xs);
  background-color: var(--tk-bg-color-overlay) !important;
  border: 1px solid var(--tk-border-color);
  border-right: none;
  border-radius: var(--tk-border-radius-md);
  box-shadow: var(--tk-shadow-md);
}

.tk-menu-popper .el-menu-item,
.tk-menu-popper .el-sub-menu__title {
  height: 40px;
  margin: 2px 0;
  font-size: var(--tk-font-size-sm);
  line-height: 40px;
  color: var(--tk-text-secondary) !important;
  background-color: transparent !important;
  border-radius: var(--tk-border-radius-sm);
}

.tk-menu-popper .el-menu-item:hover,
.tk-menu-popper .el-sub-menu__title:hover {
  color: var(--tk-text-primary) !important;
  background-color: var(--tk-bg-hover) !important;
}

.tk-menu-popper .el-menu-item.is-active {
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-link) !important;
  background-color: var(--tk-primary-color-bg) !important;
}

/* Locale switcher dropdown popper (teleported to body, must use global class name) */
.tk-lang-popper.el-popper {
  border: 1px solid var(--tk-border-color);
  border-radius: var(--tk-border-radius-md);
  box-shadow: var(--tk-shadow-md);
}

.tk-lang-popper .el-dropdown-menu {
  padding: var(--tk-spacing-xs);
  background-color: var(--tk-bg-color-overlay);
  border-radius: var(--tk-border-radius-md);
}

.tk-lang-popper .el-dropdown-menu__item {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--tk-spacing-sm);
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-secondary);
  border-radius: var(--tk-border-radius-sm);
}

.tk-lang-popper .el-dropdown-menu__item:hover {
  color: var(--tk-text-primary);
  background-color: var(--tk-bg-hover);
}

/* Mobile drawer: el-drawer teleports to body, so the default white
   background of .el-drawer / .el-drawer__body must be overridden via a
   global class. Aligns the drawer background with the sidebar token so
   light/dark themes both render without a white plate. */
.tk-default-layout__mobile-drawer.el-drawer {
  background-color: var(--tk-bg-color-sidebar) !important;
}

.tk-default-layout__mobile-drawer .el-drawer__body {
  padding: 0;
  background-color: var(--tk-bg-color-sidebar) !important;
}

/* Mobile drawer menu items: el-drawer teleports to body, so scoped :deep()
   selectors from .tk-default-layout__menu don't reach these elements.
   Apply the same padding override here so mobile menu icons align with the
   mobile logo. */
.tk-default-layout__mobile-drawer .tk-default-layout__menu--mobile .el-menu-item,
.tk-default-layout__mobile-drawer .tk-default-layout__menu--mobile .el-sub-menu__title {
  padding-right: var(--tk-spacing-lg);
  padding-left: var(--tk-spacing-lg);
}
</style>
