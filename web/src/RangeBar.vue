<script setup>
const props = defineProps({ group: Object, scale: Number })
const x = (rate) => `${(rate / props.scale) * 100}%`
</script>

<template>
  <div class="bar">
    <template v-if="group.values.length >= 5">
      <span class="whisker" :style="{ left: x(group.min), width: x(group.max - group.min) }"></span>
      <span class="box" :style="{ left: x(group.p25), width: x(group.p75 - group.p25) }"></span>
      <span class="median" :style="{ left: x(group.median) }"></span>
    </template>
    <span v-for="(v, i) in group.values.length < 5 ? group.values : []" :key="i" class="dot" :style="{ left: x(v) }"></span>
    <span v-for="m in group.lens_marks" :key="m.rate" class="mark" :style="{ left: x(m.rate) }">▲</span>
  </div>
</template>
