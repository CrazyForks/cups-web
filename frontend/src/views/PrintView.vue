<template>
  <div class="p-3 sm:p-4 md:p-6 max-w-7xl mx-auto">
    <!-- 顶部标题栏 -->
    <div class="flex items-center justify-between mb-3">
      <h1 class="text-lg font-bold flex items-center gap-2">
        <UIcon name="i-lucide-printer" class="w-5 h-5 text-primary" />
        打印
      </h1>
      <UButton variant="ghost" size="sm" icon="i-lucide-refresh-cw" @click="refreshAll" :loading="refreshing">刷新</UButton>
    </div>

    <!-- 主体两栏布局 -->
    <div class="grid grid-cols-1 lg:grid-cols-5 gap-4">
      <!-- 左栏：打印设置 + 预览 -->
      <div class="lg:col-span-3 space-y-4">
        <PrinterSelector v-model="printer" :printers="printers" @change="onPrinterChange" />
        <FileUpload
          :selected-file="selectedFile"
          :converting="converting"
          :converted="converted"
          :preview-url="previewUrl"
          :download-name="downloadName"
          :pdf-blob="pdfBlob"
          :can-print="canPrint"
          :can-convert="canConvert"
          :printing="printing"
          @file-selected="processFile"
          @clear="clearFile"
          @convert="convertToPdf"
          @print="uploadAndPrint"
        />
        <PrintOptions
          v-model:isColor="isColor"
          v-model:duplex="duplex"
          v-model:orientation="orientation"
          v-model:copies="copies"
          v-model:paperSize="paperSize"
          v-model:paperType="paperType"
          v-model:printScaling="printScaling"
          v-model:pageRange="pageRange"
          v-model:mirror="mirror"
          :printing="printing"
          :can-print="canPrint"
          :selected-file="selectedFile"
          :preview-url="previewUrl"
          :preview-type="previewType"
          :text-preview="textPreview"
          :paper-size-label="paperSizeLabel"
          :orientation-label="orientationLabel"
          :paper-dim-text="paperDimText"
          :paper-preview-style="paperPreviewStyle"
          @print="uploadAndPrint"
        />
      </div>

      <!-- 右栏：打印记录 + 打印机状态 -->
      <div class="lg:col-span-2 space-y-4">
        <PrintRecordList :records="printRecords" :loading="loadingRecords" @refresh="loadPrintRecords" />
        <PrinterStatus :printer-info="printerInfo" :printer-uri="printer" :loading="loadingPrinterInfo" :error="printerInfoError" @refresh="loadPrinterInfo" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { jsPDF } from 'jspdf'
import { getCSRF } from '../utils/api'
import { isOfficeFile, isOFDFile } from '../utils/file'
import PrinterSelector from '../components/print/PrinterSelector.vue'
import FileUpload from '../components/print/FileUpload.vue'
import PrintOptions from '../components/print/PrintOptions.vue'
import PrintRecordList from '../components/print/PrintRecordList.vue'
import PrinterStatus from '../components/print/PrinterStatus.vue'

const emit = defineEmits(['logout'])
const toast = useToast()

// ─── 打印机 ───────────────────────────────────────────────
const printer = ref('')
const printers = ref([])

// ─── 文件 ─────────────────────────────────────────────────
const selectedFile = ref(null)
const previewUrl = ref('')
const previewType = ref('')
const textPreview = ref('')
const converting = ref(false)
const converted = ref(false)
const pdfBlob = ref(null)
const downloadName = ref('')

// ─── 打印参数 ─────────────────────────────────────────────
const isColor = ref(true)
const duplex = ref('one-sided')
const orientation = ref('portrait')
const copies = ref(1)
const paperSize = ref('A4')
const paperType = ref('plain')
const printScaling = ref('fit')
const pageRange = ref('')
const mirror = ref(false)

// ─── 状态 ─────────────────────────────────────────────────
const printing = ref(false)
const refreshing = ref(false)

// ─── 打印记录 ─────────────────────────────────────────────
const printRecords = ref([])
const loadingRecords = ref(false)

// ─── 打印机状态 ───────────────────────────────────────────
const printerInfo = ref(null)
const loadingPrinterInfo = ref(false)
const printerInfoError = ref('')

// ─── 纸张尺寸映射 ─────────────────────────────────────────
const paperDimensionsMap = {
  'A5': { width: 148, height: 210 },
  'A4': { width: 210, height: 297 },
  'A3': { width: 297, height: 420 },
  'A2': { width: 420, height: 594 },
  'A1': { width: 594, height: 841 },
  '5inch': { width: 89, height: 127 },
  '6inch': { width: 102, height: 152 },
  '7inch': { width: 127, height: 178 },
  '8inch': { width: 152, height: 203 },
  '10inch': { width: 203, height: 254 },
  'Letter': { width: 216, height: 279 },
  'Legal': { width: 216, height: 356 },
}

// ─── 选项列表（供 PrintOptions 内部的 paperSizeLabel 等计算使用） ──
const orientationItems = [
  { label: '纵向', value: 'portrait' },
  { label: '横向', value: 'landscape' }
]
const paperSizeItems = [
  { label: 'A5 (148×210mm)', value: 'A5' },
  { label: 'A4 (210×297mm)', value: 'A4' },
  { label: 'A3 (297×420mm)', value: 'A3' },
  { label: 'A2 (420×594mm)', value: 'A2' },
  { label: 'A1 (594×841mm)', value: 'A1' },
  { label: '5寸 (89×127mm)', value: '5inch' },
  { label: '6寸 (102×152mm)', value: '6inch' },
  { label: '7寸 (127×178mm)', value: '7inch' },
  { label: '8寸 (152×203mm)', value: '8inch' },
  { label: '10寸 (203×254mm)', value: '10inch' },
  { label: 'Letter (8.5×11in)', value: 'Letter' },
  { label: 'Legal (8.5×14in)', value: 'Legal' }
]

// ─── 计算属性 ─────────────────────────────────────────────
const canPrint = computed(() => !!printer.value && (!!pdfBlob.value || !!selectedFile.value))
const canConvert = computed(() => !!selectedFile.value && !converting.value && selectedFile.value.type !== 'application/pdf')

const paperSizeLabel = computed(() => {
  const item = paperSizeItems.find(i => i.value === paperSize.value)
  return item?.label || paperSize.value
})

const orientationLabel = computed(() => {
  const item = orientationItems.find(i => i.value === orientation.value)
  return item?.label || (orientation.value === 'portrait' ? '纵向' : '横向')
})

const paperDimText = computed(() => {
  const dim = paperDimensionsMap[paperSize.value]
  if (!dim) return ''
  if (orientation.value === 'landscape') {
    return `${dim.height}×${dim.width}mm`
  }
  return `${dim.width}×${dim.height}mm`
})

const paperPreviewStyle = computed(() => {
  const dim = paperDimensionsMap[paperSize.value]
  if (!dim) return {}
  const isLandscape = orientation.value === 'landscape'
  const width = isLandscape ? dim.height : dim.width
  const height = isLandscape ? dim.width : dim.height
  const ratio = width / height
  const maxHeight = 400
  const maxWidth = 600
  let displayWidth, displayHeight
  if (ratio > 1) {
    displayWidth = Math.min(maxWidth, maxHeight * ratio)
    displayHeight = displayWidth / ratio
  } else {
    displayHeight = Math.min(maxHeight, maxWidth / ratio)
    displayWidth = displayHeight * ratio
  }
  return { width: `${displayWidth}px`, height: `${displayHeight}px` }
})

// ─── 文件操作 ─────────────────────────────────────────────
function clearPreviewUrl() {
  if (previewUrl.value) {
    try { URL.revokeObjectURL(previewUrl.value) } catch (_) { /* 忽略 */ }
  }
  previewUrl.value = ''
}

function clearFile() {
  clearPreviewUrl()
  previewType.value = ''
  textPreview.value = ''
  pdfBlob.value = null
  converted.value = false
  selectedFile.value = null
  downloadName.value = ''
}

function processFile(f) {
  clearFile()
  selectedFile.value = f
  downloadName.value = f.name.replace(/\.[^/.]+$/, '') + '.pdf'

  if (f.type === 'application/pdf') {
    previewUrl.value = URL.createObjectURL(f)
    previewType.value = 'pdf'
    pdfBlob.value = f
    converted.value = true
  } else if (f.type.startsWith('image/')) {
    previewUrl.value = URL.createObjectURL(f)
    previewType.value = 'image'
  } else if (isOfficeFile(f)) {
    previewType.value = 'text'
    textPreview.value = 'Office 文档（无法直接预览）。点击"转换为 PDF"生成预览。'
  } else if (isOFDFile(f)) {
    previewType.value = 'text'
    textPreview.value = 'OFD文件（开放版式文档）无法直接预览。点击"转换为PDF"生成预览。'
  } else if (f.type.startsWith('text/') || /\.(txt|md|html)$/i.test(f.name)) {
    const reader = new FileReader()
    reader.onload = () => {
      textPreview.value = reader.result
      previewType.value = 'text'
    }
    reader.readAsText(f)
  } else {
    previewType.value = 'text'
    textPreview.value = '无法预览此文件类型，可直接提交打印。'
  }
}

async function imageFileToPdfBlob(file, orient, pSize) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => {
      const dims = paperDimensionsMap[pSize] || { width: 210, height: 297 }
      const isLandscape = orient === 'landscape'
      const doc = new jsPDF({
        orientation: isLandscape ? 'l' : 'p',
        unit: 'mm',
        format: [dims.width, dims.height]
      })
      const pageWidth = doc.internal.pageSize.getWidth()
      const pageHeight = doc.internal.pageSize.getHeight()
      const margin = 10
      const maxW = pageWidth - margin * 2
      const maxH = pageHeight - margin * 2
      const imgRatio = img.width / img.height
      let drawW, drawH
      if (imgRatio > maxW / maxH) { drawW = maxW; drawH = maxW / imgRatio }
      else { drawH = maxH; drawW = maxH * imgRatio }
      const x = margin + (maxW - drawW) / 2
      const y = margin + (maxH - drawH) / 2
      doc.addImage(img, 'JPEG', x, y, drawW, drawH)
      resolve(doc.output('blob'))
    }
    img.onerror = () => reject(new Error('图片加载失败'))
    img.src = URL.createObjectURL(file)
  })
}

function textToPdfBlob(text, orient, pSize) {
  const dims = paperDimensionsMap[pSize] || { width: 210, height: 297 }
  const isLandscape = orient === 'landscape'
  const doc = new jsPDF({
    orientation: isLandscape ? 'l' : 'p',
    unit: 'mm',
    format: [dims.width, dims.height]
  })
  const pageWidth = doc.internal.pageSize.getWidth()
  const margin = 15
  const maxWidth = pageWidth - margin * 2
  const lines = doc.splitTextToSize(text || '', maxWidth)
  doc.text(lines, margin, margin)
  return doc.output('blob')
}

async function convertOfficeToPdf(file) {
  const fd = new FormData()
  fd.append('file', file, file.name)
  const resp = await fetch('/api/convert', {
    method: 'POST',
    body: fd,
    credentials: 'include',
    headers: { 'X-CSRF-Token': getCSRF() }
  })
  if (!resp.ok) throw new Error('服务端转换失败：' + await resp.text())
  return resp.blob()
}

async function convertToPdf() {
  if (!selectedFile.value) return
  converting.value = true
  try {
    const f = selectedFile.value
    let blob
    if (isOfficeFile(f) || isOFDFile(f)) {
      blob = await convertOfficeToPdf(f)
    } else if (f.type.startsWith('image/')) {
      blob = await imageFileToPdfBlob(f, orientation.value, paperSize.value)
    } else {
      const text = await f.text()
      blob = textToPdfBlob(text, orientation.value, paperSize.value)
    }
    pdfBlob.value = blob
    clearPreviewUrl()
    previewUrl.value = URL.createObjectURL(blob)
    previewType.value = 'pdf'
    converted.value = true
    toast.add({ title: '转换成功', color: 'success', icon: 'i-lucide-check-circle' })
  } catch (e) {
    toast.add({ title: '转换失败', description: e.message, color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    converting.value = false
  }
}

// ─── 打印 ─────────────────────────────────────────────────
async function uploadAndPrint() {
  if (!printer.value) { toast.add({ title: '请选择打印机', color: 'warning' }); return }

  const fileToSend = pdfBlob.value || selectedFile.value
  const filename = pdfBlob.value
    ? (downloadName.value || 'document.pdf')
    : (selectedFile.value ? selectedFile.value.name : 'document')

  const form = new FormData()
  form.append('file', fileToSend, filename)
  form.append('printer', printer.value)
  form.append('duplex', duplex.value === 'one-sided' ? 'false' : 'true')
  form.append('color', isColor.value ? 'true' : 'false')
  form.append('copies', String(copies.value))
  form.append('orientation', orientation.value)
  form.append('paper_size', paperSize.value)
  form.append('paper_type', paperType.value)
  form.append('print_scaling', printScaling.value)
  if (pageRange.value.trim()) form.append('page_range', pageRange.value.trim())
  if (mirror.value) form.append('mirror', 'true')

  printing.value = true
  try {
    const resp = await fetch('/api/print', {
      method: 'POST',
      body: form,
      credentials: 'include',
      headers: { 'X-CSRF-Token': getCSRF() }
    })
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}))
      if (resp.status === 401) emit('logout')
      throw new Error(data.error || resp.statusText)
    }
    const j = await resp.json()
    toast.add({
      title: '打印任务已提交',
      description: `任务ID：${j.jobId || '—'}，共 ${j.pages} 页`,
      color: 'success',
      icon: 'i-lucide-check-circle'
    })
    localStorage.setItem('last_printer', printer.value)
    await loadPrintRecords()
  } catch (e) {
    toast.add({ title: '打印失败', description: e.message, color: 'error', icon: 'i-lucide-x-circle' })
  } finally {
    printing.value = false
  }
}

// ─── 打印记录 ─────────────────────────────────────────────
async function loadPrintRecords(silent = false) {
  if (!silent) loadingRecords.value = true
  try {
    const resp = await fetch('/api/print-records', { credentials: 'include' })
    if (resp.ok) {
      const data = await resp.json()
      printRecords.value = (data || []).map(r => ({
        id: r.id, filename: r.filename, printerUri: r.printerUri,
        pages: r.pages, status: r.status, isColor: r.isColor,
        isDuplex: r.isDuplex, jobId: r.jobId, createdAt: r.createdAt
      }))
    } else if (resp.status === 401) {
      emit('logout')
    }
  } catch (e) {
    console.error('加载打印记录失败', e)
  } finally {
    loadingRecords.value = false
  }
}

// ─── 打印机状态 ───────────────────────────────────────────
async function loadPrinterInfo(silent = false) {
  if (!printer.value) return
  if (!silent) loadingPrinterInfo.value = true
  printerInfoError.value = ''
  try {
    const resp = await fetch(`/api/printer-info?uri=${encodeURIComponent(printer.value)}`, { credentials: 'include' })
    if (resp.ok) {
      printerInfo.value = await resp.json()
    } else if (resp.status === 401) {
      emit('logout')
    } else {
      const d = await resp.json().catch(() => ({}))
      printerInfoError.value = d.error || '查询失败'
    }
  } catch (_) {
    printerInfoError.value = '无法连接到打印机'
  } finally {
    loadingPrinterInfo.value = false
  }
}

function onPrinterChange() {
  printerInfo.value = null
  printerInfoError.value = ''
  loadPrinterInfo()
}

async function refreshAll() {
  refreshing.value = true
  await Promise.all([loadPrintRecords(true), loadPrinterInfo(true)])
  refreshing.value = false
}

// ─── 定时器 ───────────────────────────────────────────────
let recordsTimer = null
let printerInfoTimer = null

// ─── 生命周期 ─────────────────────────────────────────────
onMounted(async () => {
  try {
    const resp = await fetch('/api/printers', { credentials: 'include' })
    if (resp.ok) {
      printers.value = await resp.json()
      const last = localStorage.getItem('last_printer')
      if (last && printers.value.some(p => p.uri === last)) {
        printer.value = last
      } else if (printers.value.length > 0) {
        printer.value = printers.value[0].uri
      }
      if (printer.value) loadPrinterInfo()
    } else if (resp.status === 401) {
      emit('logout')
    }
  } catch (e) {
    toast.add({ title: '加载打印机失败', description: e.message, color: 'error' })
  }

  await loadPrintRecords()
  recordsTimer = setInterval(() => loadPrintRecords(true), 5000)
  printerInfoTimer = setInterval(() => loadPrinterInfo(true), 15000)
})

onUnmounted(() => {
  clearInterval(recordsTimer)
  clearInterval(printerInfoTimer)
  clearPreviewUrl()
})
</script>
