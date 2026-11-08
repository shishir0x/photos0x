<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  History,
  Trash2,
  Download,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  FileSpreadsheet,
  RefreshCw,
} from '@lucide/vue'
import { ConfigManager } from '../../bindings/app/core'
import type { UploadSession } from '../../bindings/app/core/models'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Clipboard } from '@wailsio/runtime'
import { toast } from 'vue-sonner'

const sessions = ref<UploadSession[]>([])
const loading = ref(true)
const expandedSessionId = ref<string | null>(null)
const loadingDetails = ref<string | null>(null)
const sessionDetailsMap = ref<Map<string, UploadSession>>(new Map())

async function loadHistory() {
  loading.value = true
  try {
    const list = await ConfigManager.GetUploadHistory()
    sessions.value = list || []
  } catch (err) {
    console.error('Failed to load history:', err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadHistory()
})

async function toggleExpand(session: UploadSession) {
  if (expandedSessionId.value === session.id) {
    expandedSessionId.value = null
    return
  }

  expandedSessionId.value = session.id
  if (!sessionDetailsMap.value.has(session.id)) {
    loadingDetails.value = session.id
    try {
      const details = await ConfigManager.GetUploadSessionDetails(session.id)
      if (details) {
        sessionDetailsMap.value.set(session.id, details)
      }
    } catch (err) {
      console.error('Failed to load session details:', err)
    } finally {
      loadingDetails.value = null
    }
  }
}

async function exportCSV(sessionId: string) {
  try {
    const csv = await ConfigManager.ExportUploadSessionCSV(sessionId)
    if (!csv) {
      toast.error('No data to export')
      return
    }

    // Try clipboard first
    await Clipboard.SetText(csv)
    toast.success('CSV copied to clipboard!')
  } catch (err) {
    console.error('Failed to export CSV:', err)
    toast.error('Failed to export CSV')
  }
}

async function clearHistory() {
  try {
    await ConfigManager.ClearUploadHistory()
    sessions.value = []
    sessionDetailsMap.value.clear()
    expandedSessionId.value = null
    toast.success('Upload history cleared')
  } catch (err) {
    console.error('Failed to clear history:', err)
    toast.error('Failed to clear history')
  }
}

function formatDate(timestamp: number): string {
  if (!timestamp) return '--'
  const d = new Date(timestamp * 1000)
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}
</script>

<template>
  <div class="flex flex-col h-[520px] max-h-[85vh] p-4 text-foreground select-none">
    <!-- Header -->
    <div class="flex items-center justify-between pb-3 border-b">
      <div class="flex items-center gap-2">
        <History class="size-4 text-primary" />
        <h2 class="text-base font-semibold">Upload History</h2>
      </div>

      <div class="flex items-center gap-1.5">
        <Button
          variant="ghost"
          size="sm"
          class="h-7 px-2 text-xs"
          title="Refresh"
          @click="loadHistory"
        >
          <RefreshCw class="size-3.5" :class="{ 'animate-spin': loading }" />
        </Button>
        <Button
          v-if="sessions.length > 0"
          variant="ghost"
          size="sm"
          class="h-7 px-2 text-xs text-destructive hover:bg-destructive/10"
          title="Clear all history"
          @click="clearHistory"
        >
          <Trash2 class="size-3.5 mr-1" />
          Clear
        </Button>
      </div>
    </div>

    <!-- Content -->
    <ScrollArea class="flex-1 mt-3 pr-2">
      <div v-if="loading && sessions.length === 0" class="flex items-center justify-center h-40 text-xs text-muted-foreground">
        Loading sessions...
      </div>

      <div v-else-if="sessions.length === 0" class="flex flex-col items-center justify-center h-40 text-center text-muted-foreground">
        <History class="size-8 opacity-40 mb-2" />
        <p class="text-xs">No upload sessions recorded yet.</p>
        <p class="text-[10px] opacity-75 mt-0.5">Sessions are automatically logged as you upload files.</p>
      </div>

      <div v-else class="space-y-2.5">
        <div
          v-for="session in sessions"
          :key="session.id"
          class="border rounded-lg p-3 bg-card/60 transition-colors"
        >
          <!-- Session Header Row -->
          <div
            class="flex items-center justify-between cursor-pointer"
            @click="toggleExpand(session)"
          >
            <div class="flex items-center gap-2 min-w-0">
              <button type="button" class="text-muted-foreground">
                <ChevronDown v-if="expandedSessionId === session.id" class="size-4" />
                <ChevronRight v-else class="size-4" />
              </button>

              <div class="truncate">
                <div class="flex items-center gap-2">
                  <span class="text-xs font-semibold">{{ formatDate(session.startedAt) }}</span>
                  <span v-if="session.albumName" class="text-[10px] px-1.5 py-0.2 bg-muted rounded text-muted-foreground truncate max-w-28">
                    {{ session.albumName }}
                  </span>
                </div>
                <p class="text-[10px] text-muted-foreground truncate">
                  {{ session.totalFiles }} item{{ session.totalFiles === 1 ? '' : 's' }} • {{ formatBytes(session.totalBytes) }}
                </p>
              </div>
            </div>

            <!-- Stats badges & CSV export -->
            <div class="flex items-center gap-1.5 shrink-0">
              <span v-if="session.successCount > 0" class="text-[10px] px-1.5 py-0.2 rounded bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-0.5">
                <CheckCircle2 class="size-3" /> {{ session.successCount }}
              </span>
              <span v-if="session.failCount > 0" class="text-[10px] px-1.5 py-0.2 rounded bg-rose-500/10 text-rose-600 dark:text-rose-400 font-medium flex items-center gap-0.5">
                <XCircle class="size-3" /> {{ session.failCount }}
              </span>
              <span v-if="session.skippedCount > 0" class="text-[10px] px-1.5 py-0.2 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 font-medium flex items-center gap-0.5">
                <AlertTriangle class="size-3" /> {{ session.skippedCount }}
              </span>

              <Button
                variant="ghost"
                size="sm"
                class="size-7 p-0 ml-1 text-muted-foreground hover:text-foreground"
                title="Copy CSV to clipboard"
                @click.stop="exportCSV(session.id)"
              >
                <FileSpreadsheet class="size-3.5" />
              </Button>
            </div>
          </div>

          <!-- Expandable Detail Table -->
          <div
            v-if="expandedSessionId === session.id"
            class="mt-2.5 pt-2 border-t text-left"
          >
            <div v-if="loadingDetails === session.id" class="text-xs text-muted-foreground py-2 text-center">
              Loading file details...
            </div>

            <div
              v-else-if="sessionDetailsMap.get(session.id)?.items?.length"
              class="max-h-40 overflow-y-auto space-y-1 pr-1 text-[11px]"
            >
              <div
                v-for="item in sessionDetailsMap.get(session.id)!.items"
                :key="item.id"
                class="flex items-center justify-between p-1.5 rounded bg-muted/30"
              >
                <div class="truncate mr-2">
                  <p class="truncate font-medium text-foreground">{{ item.fileName }}</p>
                  <p v-if="item.errorMessage" class="text-[9px] text-destructive truncate leading-tight">
                    {{ item.errorMessage }}
                  </p>
                  <p v-else-if="item.mediaKey" class="text-[9px] text-muted-foreground truncate leading-tight">
                    {{ item.mediaKey.slice(0, 14) }}...
                  </p>
                </div>

                <span
                  class="text-[9px] px-1 py-0.2 rounded shrink-0 font-medium uppercase"
                  :class="{
                    'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400': item.status === 'success',
                    'bg-rose-500/15 text-rose-600 dark:text-rose-400': item.status === 'fail',
                    'bg-amber-500/15 text-amber-600 dark:text-amber-400': item.status === 'skipped',
                  }"
                >
                  {{ item.status }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </ScrollArea>
  </div>
</template>
