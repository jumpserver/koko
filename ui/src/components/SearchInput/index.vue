<script setup lang="ts">
import type { ISearchOptions, SearchAddon } from '@xterm/addon-search';

import { useI18n } from 'vue-i18n';
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useElementSize, useMutationObserver } from '@vueuse/core';
import { CaseLower, CaseSensitive, ChevronDown, ChevronUp, Regex, X } from 'lucide-vue-next';

import { useColor } from '@/hooks/useColor';

const props = defineProps<{
  searchAddon: SearchAddon;
  isKubernetes?: boolean;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();

const searchOptions = reactive<ISearchOptions>({
  caseSensitive: false,
  wholeWord: false,
  regex: false,
  decorations: {
    matchBorder: '#ff8c00',
    matchOverviewRuler: '#ffff00',
    activeMatchBackground: '#ffa500',
    activeMatchBorder: '#ff8c00',
    activeMatchColorOverviewRuler: '#ffa500',
  },
});

const { t } = useI18n();
const { darken, lighten } = useColor();

const searchKey = ref('');
const gutterWidth = 16;
const drawerRef = ref<HTMLElement | null>(null);

const { width } = useElementSize(drawerRef);

const keyWordsSearch = (value: string) => {
  if (value) {
    props.searchAddon.findNext(value, searchOptions);
  }
};

const toggleSearchOption = (option: keyof typeof searchOptions) => {
  if (option in searchOptions && typeof searchOptions[option] === 'boolean') {
    (searchOptions[option] as boolean) = !(searchOptions[option] as boolean);

    if (searchKey.value) {
      keyWordsSearch(searchKey.value);
    }
  }
};

const toggleCaseSensitive = () => toggleSearchOption('caseSensitive');
const toggleWholeWord = () => toggleSearchOption('wholeWord');
const toggleRegex = () => toggleSearchOption('regex');

const getActiveStyle = (isActive: boolean) => {
  return isActive ? { backgroundColor: lighten(5) } : {};
};

const searchOptionButtons = [
  {
    key: 'caseSensitive' as const,
    icon: CaseSensitive,
    label: t('CaseSensitive'),
    toggleFn: toggleCaseSensitive,
  },
  {
    key: 'wholeWord' as const,
    icon: CaseLower,
    label: t('MatchWholeWords'),
    toggleFn: toggleWholeWord,
  },
  {
    key: 'regex' as const,
    icon: Regex,
    label: t('UsingRegularExpressions'),
    toggleFn: toggleRegex,
  },
];

const handleSearchPrevious = () => {
  props.searchAddon.findPrevious(searchKey.value, searchOptions);
};

const handleSearchNext = () => {
  props.searchAddon.findNext(searchKey.value, searchOptions);
};

const closeSearch = (shouldEmit = false) => {
  searchKey.value = '';
  props.searchAddon.clearDecorations();
  props.searchAddon.clearActiveDecoration();

  if (shouldEmit) {
    emit('close');
  }
};

const handleCloseSearch = () => closeSearch(true);

const positionRight = computed(() => {
  const w = width.value;

  if (!drawerRef.value || w === 0) return `${gutterWidth}px`;

  const clamped = Math.min(800, Math.max(600, Math.round(w)));
  return `calc(${clamped}px + ${gutterWidth}px)`;
});

watch(
  () => searchKey.value,
  value => {
    if (!value) {
      props.searchAddon.clearDecorations();
      props.searchAddon.clearActiveDecoration();
    }
  }
);

onMounted(() => {
  drawerRef.value = document.getElementById('drawer-inner-target');
});

useMutationObserver(
  () => document.body,
  () => {
    const el = document.getElementById('drawer-inner-target');

    if (el !== drawerRef.value) {
      drawerRef.value = el;
    }
  },
  {
    childList: true,
    subtree: true,
  }
);
</script>

<template>
  <div
    class="absolute flex items-center right-2 p-2 z-999 shadow-md rounded-md"
    :style="{ backgroundColor: lighten(10), top: isKubernetes ? '3rem' : '0.5rem', right: positionRight }"
  >
    <n-flex align="center" class="!gap-2">
      <n-input
        v-model:value="searchKey"
        clearable
        size="small"
        :style="{ width: '45%', flex: '1' }"
        @update:value="keyWordsSearch"
      />

      <n-button quaternary size="tiny" @click="handleSearchPrevious">
        <template #icon>
          <ChevronUp />
        </template>
      </n-button>
      <n-button quaternary size="tiny" @click="handleSearchNext">
        <template #icon>
          <ChevronDown />
        </template>
      </n-button>
    </n-flex>

    <n-divider vertical :style="{ height: '24px', backgroundColor: darken(1) }" />

    <n-flex align="center" class="!flex-nowrap !gap-2">
      <n-flex align="center" class="!flex-nowrap !gap-2">
        <div
          v-for="option in searchOptionButtons"
          :key="option.key"
          class="search-option-btn"
          :class="{ active: searchOptions[option.key] }"
          :style="getActiveStyle(Boolean(searchOptions[option.key]))"
          :title="option.label"
          tabindex="0"
          @click="option.toggleFn"
        >
          <component :is="option.icon" color="#fff" :size="16" />
        </div>
      </n-flex>

      <n-button quaternary size="tiny" @click="handleCloseSearch">
        <template #icon>
          <X />
        </template>
      </n-button>
    </n-flex>
  </div>
</template>

<style scoped lang="scss">
.search-option-btn {
  padding: 4px 6px;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
  background-color: transparent;

  &:hover {
    background-color: rgba(255, 255, 255, 0.1);
    transform: scale(1.05);
  }

  &:active {
    background-color: rgba(255, 255, 255, 0.2);
    transform: scale(0.95);
  }
}
</style>
