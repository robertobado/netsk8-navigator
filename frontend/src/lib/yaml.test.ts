import { describe, expect, it } from 'vitest'
import { checkYamlSyntax } from './yaml'

describe('checkYamlSyntax', () => {
  it('returns null for valid YAML', () => {
    expect(checkYamlSyntax('apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n')).toBeNull()
  })

  it('returns null for an empty document', () => {
    expect(checkYamlSyntax('')).toBeNull()
  })

  it('returns the line/column of a syntax error', () => {
    const err = checkYamlSyntax('foo: bar\n  bad: nested\n')
    expect(err).not.toBeNull()
    expect(err?.line).toBe(1)
    expect(err?.message.length).toBeGreaterThan(0)
  })

  it('flags a tab character used for indentation', () => {
    const err = checkYamlSyntax('foo:\n\tbar: baz\n')
    expect(err).not.toBeNull()
  })

  it('flags an unterminated quoted string', () => {
    const err = checkYamlSyntax('name: "unterminated\n')
    expect(err).not.toBeNull()
  })
})
