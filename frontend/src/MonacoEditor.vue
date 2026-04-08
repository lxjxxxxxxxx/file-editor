<template>
  <div ref="editorContainer" class="monaco-editor-container" :style="{ height }"></div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import * as monaco from 'monaco-editor'

const props = defineProps({
  path: { type: String, default: '' },
  code: { type: String, default: '' },
  options: { type: Object, default: () => ({}) },
  height: { type: String, default: '100%' },
})

const emit = defineEmits(['change', 'cursor-change'])

const editorContainer = ref(null)
let editor = null
let isInternalChange = false

function getLanguageFromPath(filePath) {
  const ext = filePath.split('.').pop().toLowerCase()
  const map = {
    js: 'javascript', ts: 'typescript', jsx: 'javascript', tsx: 'typescript',
    vue: 'html', html: 'html', htm: 'html', xml: 'xml', svg: 'xml',
    css: 'css', scss: 'scss', sass: 'scss', less: 'less',
    json: 'json', json5: 'json',
    py: 'python', rb: 'ruby', java: 'java', kt: 'kotlin',
    go: 'go', rs: 'rust', c: 'c', cpp: 'cpp', h: 'c',
    php: 'php', sql: 'sql', sh: 'shell', bash: 'shell', zsh: 'shell',
    bat: 'bat', ps1: 'powershell',
    yaml: 'yaml', yml: 'yaml', toml: 'ini', ini: 'ini', cfg: 'ini', conf: 'ini',
    md: 'markdown', txt: 'plaintext', log: 'plaintext',
    lua: 'lua', pl: 'perl', csv: 'plaintext', tsv: 'plaintext',
    env: 'plaintext',
    dockerfile: 'dockerfile',
  }
  return map[ext] || 'plaintext'
}

function createEditor() {
  if (editor) editor.dispose()
  editor = monaco.editor.create(editorContainer.value, {
    value: props.code,
    language: getLanguageFromPath(props.path),
    theme: 'vs-dark',
    automaticLayout: true,
    minimap: { enabled: true },
    fontSize: 14,
    tabSize: 2,
    wordWrap: 'on',
    scrollBeyondLastLine: false,
    folding: true,
    lineNumbers: 'on',
    renderWhitespace: 'selection',
    ...props.options,
  })

  editor.onDidChangeModelContent(() => {
    if (!isInternalChange) {
      emit('change', editor.getValue())
    }
  })

  editor.onDidChangeCursorPosition((e) => {
    emit('cursor-change', e.position)
  })
}

function updateContent(newCode) {
  if (!editor) return
  isInternalChange = true
  const model = editor.getModel()
  if (model) {
    if (model.getValue() !== newCode) {
      model.setValue(newCode)
    }
  }
  const lang = getLanguageFromPath(props.path)
  monaco.editor.setModelLanguage(model, lang)
  isInternalChange = false
}

onMounted(() => {
  createEditor()
})

onUnmounted(() => {
  if (editor) editor.dispose()
})

watch(() => props.code, (newCode) => {
  updateContent(newCode)
})

watch(() => props.path, () => {
  if (editor) {
    const lang = getLanguageFromPath(props.path)
    const model = editor.getModel()
    if (model) monaco.editor.setModelLanguage(model, lang)
  }
})
</script>

<style scoped>
.monaco-editor-container {
  width: 100%;
}
</style>
