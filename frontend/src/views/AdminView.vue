<template>
  <div class="p-6 space-y-6">
    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-users" class="w-5 h-5" />
          用户管理
        </h2>
      </template>
      <UForm @submit="saveUser" :state="form" class="grid grid-cols-1 md:grid-cols-4 gap-3">
        <UInput v-model="form.username" :disabled="form.protected" placeholder="登录名" />
        <UInput type="password" v-model="form.password" :placeholder="isEditing ? '留空不修改密码' : '密码'" />
        <USelect
          v-model="form.role"
          :disabled="form.protected"
          :items="roleItems"
          value-key="value"
          label-key="label"
        />
        <UInput v-model="form.contactName" placeholder="联系人" />
        <UInput v-model="form.phone" placeholder="联系电话" />
        <UInput v-model="form.email" placeholder="邮箱" />
        <div class="flex gap-2">
          <UButton type="submit" color="primary">{{ isEditing ? '保存' : '新增用户' }}</UButton>
          <UButton type="button" variant="ghost" @click="resetForm">重置</UButton>
        </div>
      </UForm>

      <div class="overflow-x-auto mt-4">
        <UTable :columns="userColumns" :data="users">
          <template #actions-cell="{ row }">
            <div class="flex gap-2">
              <UButton size="xs" variant="ghost" @click="editUser(row)">编辑</UButton>
              <UButton size="xs" variant="outline" color="error" :disabled="row.username === 'admin'" @click="deleteUser(row)">删除</UButton>
            </div>
          </template>
        </UTable>
      </div>
    </UCard>

    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-file-text" class="w-5 h-5" />
          打印记录
        </h2>
      </template>
      <div class="flex flex-wrap gap-3 items-end mb-4">
        <UInput v-model="printFilters.username" placeholder="用户名" />
        <UInput type="date" v-model="printFilters.start" />
        <UInput type="date" v-model="printFilters.end" />
        <UButton variant="outline" @click="loadPrintRecords" icon="i-lucide-search">查询</UButton>
      </div>
      <div class="overflow-x-auto">
        <UTable :columns="printColumns" :data="printRecords">
          <template #download-cell="{ row }">
            <UButton size="xs" variant="ghost" :href="`/api/print-records/${row.id}/file`" target="_blank" icon="i-lucide-download">下载</UButton>
          </template>
        </UTable>
      </div>
    </UCard>

    <UCard>
      <template #header>
        <h2 class="text-xl font-bold flex items-center gap-2">
          <UIcon name="i-lucide-settings" class="w-5 h-5" />
          系统设置
        </h2>
      </template>
      <div class="grid grid-cols-1 md:grid-cols-4 gap-3 items-end">
        <div>
          <label class="block text-sm font-medium mb-1">自动清理天数</label>
          <UInput type="number" step="1" v-model="settings.retentionDays" placeholder="例如 30" />
        </div>
        <div class="flex items-end">
          <UButton color="primary" @click="saveSettings" icon="i-lucide-save">保存设置</UButton>
        </div>
      </div>
      <div class="text-sm text-muted mt-2">自动清理会删除打印记录与文件，并压缩数据库。</div>
    </UCard>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'

const emit = defineEmits(['logout'])

const users = ref([])
const form = ref({
  id: null,
  username: '',
  password: '',
  role: 'user',
  protected: false,
  contactName: '',
  phone: '',
  email: ''
})
const printFilters = ref({ username: '', start: '', end: '' })
const printRecords = ref([])
const settings = ref({ retentionDays: '' })

const isEditing = computed(() => !!form.value.id)

const roleItems = [
  { label: '普通用户', value: 'user' },
  { label: '管理员', value: 'admin' }
]

const userColumns = [
  { accessorKey: 'id', header: 'ID' },
  { accessorKey: 'username', header: '登录名' },
  { accessorKey: 'role', header: '角色' },
  { accessorKey: 'contactName', header: '联系人' },
  { accessorKey: 'phone', header: '电话' },
  { accessorKey: 'email', header: '邮箱' },
  { id: 'actions', header: '操作' }
]

const printColumns = [
  { accessorKey: 'createdAt', header: '时间' },
  { accessorKey: 'username', header: '用户' },
  { accessorKey: 'filename', header: '文件' },
  { accessorKey: 'pages', header: '页数' },
  { accessorKey: 'status', header: '状态' },
  { id: 'download', header: '下载' }
]

function getCSRF() {
  const m = document.cookie.match('(^|;)\\s*csrf_token\\s*=\\s*([^;]+)')
  return m ? m.pop() : ''
}

async function readError(resp) {
  try {
    const data = await resp.json()
    return data.error || resp.statusText
  } catch (e) {
    try {
      const text = await resp.text()
      return text || resp.statusText
    } catch (err) {
      return resp.statusText
    }
  }
}

function resetForm() {
  form.value = {
    id: null,
    username: '',
    password: '',
    role: 'user',
    protected: false,
    contactName: '',
    phone: '',
    email: ''
  }
}

function editUser(user) {
  form.value = {
    id: user.id,
    username: user.username,
    password: '',
    role: user.role,
    protected: user.username === 'admin',
    contactName: user.contactName || '',
    phone: user.phone || '',
    email: user.email || ''
  }
}

async function loadUsers() {
  const resp = await fetch('/api/admin/users', { credentials: 'include' })
  if (!resp.ok) {
    if (resp.status === 401) emit('logout')
    return
  }
  users.value = await resp.json()
}

async function saveUser() {
  const payload = {
    username: form.value.username,
    password: form.value.password,
    role: form.value.role,
    contactName: form.value.contactName,
    phone: form.value.phone,
    email: form.value.email
  }
  const url = isEditing.value ? `/api/admin/users/${form.value.id}` : '/api/admin/users'
  const method = isEditing.value ? 'PUT' : 'POST'
  const resp = await fetch(url, {
    method,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': getCSRF()
    },
    body: JSON.stringify(payload)
  })
  if (!resp.ok) {
    const msg = await readError(resp)
    alert(msg)
    if (resp.status === 401) emit('logout')
    return
  }
  await loadUsers()
  resetForm()
}

async function deleteUser(user) {
  if (!confirm(`确认删除用户 ${user.username} ?`)) return
  const resp = await fetch(`/api/admin/users/${user.id}`, {
    method: 'DELETE',
    credentials: 'include',
    headers: { 'X-CSRF-Token': getCSRF() }
  })
  if (!resp.ok) {
    const msg = await readError(resp)
    alert(msg)
    if (resp.status === 401) emit('logout')
    return
  }
  await loadUsers()
}

async function loadPrintRecords() {
  const params = new URLSearchParams()
  if (printFilters.value.username) params.set('username', printFilters.value.username)
  if (printFilters.value.start) params.set('start', printFilters.value.start)
  if (printFilters.value.end) params.set('end', printFilters.value.end)
  const resp = await fetch(`/api/admin/print-records?${params.toString()}`, { credentials: 'include' })
  if (!resp.ok) {
    if (resp.status === 401) emit('logout')
    return
  }
  printRecords.value = await resp.json()
}

async function loadSettings() {
  const resp = await fetch('/api/admin/settings', { credentials: 'include' })
  if (!resp.ok) {
    if (resp.status === 401) emit('logout')
    return
  }
  const data = await resp.json()
  settings.value.retentionDays = String(data.retentionDays || 0)
}

async function saveSettings() {
  const payload = {
    retentionDays: parseInt(settings.value.retentionDays || '0', 10)
  }
  const resp = await fetch('/api/admin/settings', {
    method: 'PUT',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': getCSRF()
    },
    body: JSON.stringify(payload)
  })
  if (!resp.ok) {
    const msg = await readError(resp)
    alert(msg)
    if (resp.status === 401) emit('logout')
    return
  }
  await loadSettings()
}

onMounted(async () => {
  await loadUsers()
  await loadPrintRecords()
  await loadSettings()
})
</script>