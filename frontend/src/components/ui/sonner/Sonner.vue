<script lang="ts" setup>
import type { ToasterProps } from "vue-sonner"
import { Toaster as Sonner } from "vue-sonner"
import { X } from "@lucide/vue"

const props = withDefaults(defineProps<ToasterProps>(), {
  closeButton: true,
  closeButtonPosition: 'top-right',
  duration: 3000,
})
</script>

<template>
  <Sonner
    class="toaster group"
    v-bind="props"
    :style="{
      '--normal-bg': 'var(--popover)',
      '--normal-text': 'var(--popover-foreground)',
      '--normal-border': 'var(--border)',
    }"
  >
    <template #close-icon>
      <slot name="close-icon">
        <X class="size-3" />
      </slot>
    </template>
  </Sonner>
</template>
