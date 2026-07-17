import { describe, expect, it } from 'vitest'
import { fixMarkdown } from './MarkdownRenderer'

describe('fixMarkdown', () => {
  it('normalizes compact headings and lists', () => {
    const output = fixMarkdown('##核心观点###数据概况-**文件名称**：users.csv-**字段定义**：姓名、城市')

    expect(output).toContain('## 核心观点')
    expect(output).toContain('### 数据概况')
    expect(output).toContain('- **文件名称**：users.csv')
    expect(output).toContain('- **字段定义**：姓名、城市')
  })

  it('repairs compact markdown tables from model output', () => {
    const output = fixMarkdown(
      '###关键人员信息摘要|姓名|城市|薪资||------|------|------||张三|上海|24000||李四|北京|18000|',
    )

    expect(output).toContain('### 关键人员信息摘要')
    expect(output).toContain('| 姓名 | 城市 | 薪资 |')
    expect(output).toContain('| --- | --- | --- |')
    expect(output).toContain('| 张三 | 上海 | 24000 |')
    expect(output).toContain('| 李四 | 北京 | 18000 |')
  })

  it('keeps valid mermaid fences readable', () => {
    const output = fixMarkdown('```mermaidflowchart TD A-->B```')

    expect(output).toContain('```mermaid')
    expect(output).toContain('flowchart TD')
    expect(output).toMatch(/A\s*-->\s*B/)
    expect(output).toMatch(/```\s*$/)
  })

  it('removes chat template tokens and think tags', () => {
    const output = fixMarkdown('<|im_start|>assistant\n<think>hidden</think>\n##答案\n正文<|im_end|>')

    expect(output).not.toContain('<|im_start|>')
    expect(output).not.toContain('<think>')
    expect(output).not.toContain('hidden')
    expect(output).toContain('## 答案')
  })
})
