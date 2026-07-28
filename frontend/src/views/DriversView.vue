<template>
  <div class="p-3 sm:p-4 md:p-6 space-y-4 md:space-y-6">
    <!-- 自动检测打印机 -->
    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-scan-search" class="w-5 h-5" />
          自动检测打印机
        </h2>
      </template>
      <div class="space-y-4">
        <UButton
          icon="i-lucide-scan-search"
          :loading="scanning"
          :disabled="scanning"
          @click="detectPrinters"
        >
          扫描打印机
        </UButton>
        <div v-if="scanning" class="flex items-center gap-2 text-sm text-muted">
          <UIcon name="i-lucide-loader-circle" class="w-4 h-4 animate-spin" />
          正在扫描，请稍候…
        </div>
        <div v-if="detected.length" class="overflow-x-auto">
          <UTable :columns="detectColumns" :data="detected">
            <template #connection-cell="{ row }">
              <div class="flex items-center gap-1">
                <UIcon
                  :name="row.original.connection === 'usb' ? 'i-lucide-usb' : 'i-lucide-wifi'"
                  class="w-4 h-4"
                />
                <span>{{ row.original.connection === 'usb' ? 'USB' : '网络' }}</span>
              </div>
            </template>
            <template #printer-cell="{ row }">
              {{ row.original.manufacturer }} {{ row.original.model }}
            </template>
            <template #driverStatus-cell="{ row }">
              <UBadge v-if="row.original.hasDriver" color="success" size="sm">已就绪</UBadge>
              <UBadge v-else-if="row.original.driverMatch" color="warning" size="sm">推荐安装: {{ row.original.driverMatch }}</UBadge>
              <UBadge v-else color="neutral" size="sm">未知</UBadge>
            </template>
            <template #actions-cell="{ row }">
              <UButton
                v-if="row.original.driverMatch && !row.original.hasDriver"
                size="sm"
                icon="i-lucide-download"
                :loading="settingUp === row.original.uri"
                :disabled="!!settingUp"
                @click="setupPrinter(row.original, true)"
              >
                一键安装并添加
              </UButton>
              <UButton
                v-else-if="row.original.hasDriver"
                size="sm"
                variant="outline"
                icon="i-lucide-plus"
                :loading="settingUp === row.original.uri"
                :disabled="!!settingUp"
                @click="setupPrinter(row.original, false)"
              >
                添加打印机
              </UButton>
            </template>
          </UTable>
        </div>
        <div v-else-if="scanDone && !detected.length" class="text-sm text-muted">
          未检测到打印机。请检查连接后重试。
        </div>
      </div>
    </UCard>

    <!-- 驱动管理 -->
    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-puzzle" class="w-5 h-5" />
          驱动管理
        </h2>
      </template>
      <div class="overflow-x-auto">
        <UTable :columns="driverColumns" :data="drivers">
          <template #description-cell="{ row }">
            <span>{{ row.original.description }}</span>
            <UBadge v-if="row.original.needCompile" color="warning" size="xs" class="ml-1">需编译</UBadge>
          </template>
          <template #arch-cell="{ row }">
            {{ (row.original.arch || []).join(', ') }}
          </template>
          <template #status-cell="{ row }">
            <UBadge v-if="row.original.installed" color="success" size="sm">
              已安装 {{ row.original.installedAt ? formatDate(row.original.installedAt) : '' }}
            </UBadge>
            <UBadge v-else color="neutral" size="sm">未安装</UBadge>
          </template>
          <template #actions-cell="{ row }">
            <UButton
              v-if="!row.original.installed"
              size="sm"
              icon="i-lucide-download"
              :loading="installingDriver === row.original.name"
              :disabled="!!installingDriver || !!removingDriver"
              @click="confirmInstall(row.original)"
            >
              安装
            </UButton>
            <UButton
              v-else
              size="sm"
              variant="outline"
              color="error"
              icon="i-lucide-trash-2"
              :loading="removingDriver === row.original.name"
              :disabled="!!installingDriver || !!removingDriver"
              @click="confirmRemove(row.original)"
            >
              卸载
            </UButton>
          </template>
        </UTable>
      </div>
    </UCard>

    <!-- 上传自定义驱动 -->
    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-upload" class="w-5 h-5" />
          上传自定义驱动
        </h2>
      </template>
      <div class="space-y-3">
        <p class="text-sm text-muted">支持 PPD 文件 (.ppd) 或 Debian 包 (.deb)</p>
        <div class="flex flex-wrap items-center gap-3">
          <UButton variant="outline" icon="i-lucide-file-up" @click="triggerFileInput">
            选择文件
          </UButton>
          <span v-if="uploadFile" class="text-sm text-muted truncate max-w-xs">{{ uploadFile.name }}</span>
          <input
            ref="fileInputRef"
            type="file"
            accept=".ppd,.deb"
            class="hidden"
            @change="onFileSelected"
          />
        </div>
        <UButton
          v-if="uploadFile"
          color="primary"
          icon="i-lucide-upload"
          :loading="uploading"
          :disabled="uploading"
          @click="uploadDriver"
        >
          上传安装
        </UButton>
      </div>
    </UCard>

    <!-- 安装确认弹窗 -->
    <UModal v-model:open="showInstallModal">
      <template #content>
        <div class="p-6 space-y-4">
          <h3 class="text-lg font-semibold">确认安装</h3>
          <p>安装驱动 <strong>{{ pendingDriver?.displayName }}</strong>？</p>
          <p class="text-sm text-muted">需编译的驱动可能需要几分钟。</p>
          <div class="flex justify-end gap-2">
            <UButton variant="ghost" @click="showInstallModal = false">取消</UButton>
            <UButton color="primary" :loading="!!installingDriver" @click="installDriver">确认安装</UButton>
          </div>
        </div>
      </template>
    </UModal>

    <!-- 卸载确认弹窗 -->
    <UModal v-model:open="showRemoveModal">
      <template #content>
        <div class="p-6 space-y-4">
          <h3 class="text-lg font-semibold">确认卸载</h3>
          <p>确定要卸载驱动 <strong>{{ pendingDriver?.displayName }}</strong> 吗？</p>
          <p class="text-sm text-muted">卸载后使用该驱动的打印机可能无法正常工作。</p>
          <div class="flex justify-end gap-2">
            <UButton variant="ghost" @click="showRemoveModal = false">取消</UButton>
            <UButton color="error" :loading="!!removingDriver" @click="removeDriver">确认卸载</UButton>
          </div>
        </div>
      </template>
    </UModal>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch, readError } from '../utils/api'

defineProps({ session: Object })
const emit = defineEmits(['logout'])
const toast = useToast()

// --- 自动检测打印机 ---
const scanning = ref(false)
const scanDone = ref(false)
const detected = ref([])
const settingUp = ref(null)

const detectColumns = [
  { id: 'connection', header: '连接方式' },
  { id: 'printer', header: '打印机' },
  { id: 'driverStatus', header: '驱动状态' },
  { id: 'actions', header: '操作' }
]

async function detectPrinters() {
  scanning.value = true
  scanDone.value = false
  detected.value = []
  try {
    const resp = await apiFetch('/api/admin/drivers/detect', {}, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '扫描失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    detected.value = await resp.json()
  } catch (e) {
    toast.add({ title: '扫描失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    scanning.value = false
    scanDone.value = true
  }
}

async function setupPrinter(printer, installDriver) {
  settingUp.value = printer.uri
  try {
    const resp = await apiFetch('/api/admin/drivers/setup', {
      method: 'POST',
      body: JSON.stringify({
        uri: printer.uri,
        manufacturer: printer.manufacturer,
        model: printer.model,
        driverMatch: printer.driverMatch,
        installDriver
      }),
      signal: AbortSignal.timeout(300000)
    }, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '设置失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    toast.add({ title: '设置成功', description: `打印机 ${printer.manufacturer} ${printer.model} 已添加`, color: 'success', icon: 'i-lucide-check-circle' })
    await detectPrinters()
  } catch (e) {
    toast.add({ title: '设置失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    settingUp.value = null
  }
}

// --- 驱动管理 ---
const drivers = ref([])
const installingDriver = ref(null)
const removingDriver = ref(null)
const pendingDriver = ref(null)
const showInstallModal = ref(false)
const showRemoveModal = ref(false)

const driverColumns = [
  { accessorKey: 'displayName', header: '驱动名称' },
  { id: 'description', header: '说明' },
  { id: 'arch', header: '架构' },
  { id: 'status', header: '状态' },
  { id: 'actions', header: '操作' }
]

function formatDate(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

async function loadDrivers() {
  try {
    const resp = await apiFetch('/api/admin/drivers', {}, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '加载驱动列表失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    drivers.value = await resp.json()
  } catch (e) {
    toast.add({ title: '加载驱动列表失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  }
}

function confirmInstall(driver) {
  pendingDriver.value = driver
  showInstallModal.value = true
}

function confirmRemove(driver) {
  pendingDriver.value = driver
  showRemoveModal.value = true
}

async function installDriver() {
  const driver = pendingDriver.value
  if (!driver) return
  installingDriver.value = driver.name
  showInstallModal.value = false
  try {
    const resp = await apiFetch('/api/admin/drivers/install', {
      method: 'POST',
      body: JSON.stringify({ name: driver.name }),
      signal: AbortSignal.timeout(300000)
    }, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '安装失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    toast.add({ title: '安装成功', description: `驱动 ${driver.displayName} 已安装`, color: 'success', icon: 'i-lucide-check-circle' })
    await loadDrivers()
  } catch (e) {
    toast.add({ title: '安装失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    installingDriver.value = null
    pendingDriver.value = null
  }
}

async function removeDriver() {
  const driver = pendingDriver.value
  if (!driver) return
  removingDriver.value = driver.name
  showRemoveModal.value = false
  try {
    const resp = await apiFetch('/api/admin/drivers/remove', {
      method: 'POST',
      body: JSON.stringify({ name: driver.name }),
      signal: AbortSignal.timeout(300000)
    }, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '卸载失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    toast.add({ title: '卸载成功', description: `驱动 ${driver.displayName} 已卸载`, color: 'success', icon: 'i-lucide-check-circle' })
    await loadDrivers()
  } catch (e) {
    toast.add({ title: '卸载失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    removingDriver.value = null
    pendingDriver.value = null
  }
}

// --- 上传自定义驱动 ---
const fileInputRef = ref(null)
const uploadFile = ref(null)
const uploading = ref(false)

function triggerFileInput() {
  fileInputRef.value?.click()
}

function onFileSelected(e) {
  const file = e.target.files?.[0]
  if (file) {
    uploadFile.value = file
  }
}

async function uploadDriver() {
  if (!uploadFile.value) return
  uploading.value = true
  try {
    const formData = new FormData()
    formData.append('file', uploadFile.value)
    const resp = await apiFetch('/api/admin/drivers/upload', {
      method: 'POST',
      body: formData,
      signal: AbortSignal.timeout(300000)
    }, () => emit('logout'))
    if (!resp.ok) {
      const msg = await readError(resp)
      toast.add({ title: '上传失败', description: msg, color: 'error', icon: 'i-lucide-x-circle' })
      return
    }
    toast.add({ title: '上传成功', description: `驱动文件 ${uploadFile.value.name} 已安装`, color: 'success', icon: 'i-lucide-check-circle' })
    uploadFile.value = null
    if (fileInputRef.value) fileInputRef.value.value = ''
    await loadDrivers()
  } catch (e) {
    toast.add({ title: '上传失败', description: String(e), color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    uploading.value = false
  }
}

onMounted(() => {
  loadDrivers()
})
</script>
