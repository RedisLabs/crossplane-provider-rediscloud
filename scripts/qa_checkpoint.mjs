#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Encrypt immutable ownership intent before any Cloud write. No secret is printed.
import { createCipheriv, createDecipheriv, createHash, createHmac, randomBytes } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';

const AAD = Buffer.from('rediscloud-qa-ownership-intent-v1');

function keyFromEnv() {
  const raw = process.env.QA_CHECKPOINT_KEY || '';
  const key = Buffer.from(raw, 'base64');
  if (key.length !== 32 || key.toString('base64') !== raw) throw new Error('checkpoint key must be canonical base64 for 32 bytes');
  return key;
}

function validate(value) {
  if (!value || value.version !== 1 || value.repo !== 'RedisLabs/crossplane-provider-rediscloud' ||
      !/^[1-9][0-9]*$/.test(value.runId) || !/^[1-9][0-9]*$/.test(value.attempt) ||
      value.runName !== `xp-ci-${value.runId}-${value.attempt}` ||
      !/^[a-f0-9]{40}$/.test(value.commit) || !/^[a-f0-9]{64}$/.test(value.originHash) ||
      !/^[a-f0-9]{64}$/.test(value.credentialBinding) || !['strict', 'rehearsal'].includes(value.kind)) {
    throw new Error('invalid checkpoint ownership intent');
  }
}

export function encryptIntent(value, key) {
  validate(value);
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', key, iv);
  cipher.setAAD(AAD);
  const ciphertext = Buffer.concat([cipher.update(JSON.stringify(value)), cipher.final()]);
  return JSON.stringify({version: 1, iv: iv.toString('base64'), ciphertext: ciphertext.toString('base64'), tag: cipher.getAuthTag().toString('base64')});
}

export function decryptIntent(encoded, key, expectedRunId, expectedAttempt) {
  const item = JSON.parse(encoded);
  if (item.version !== 1) throw new Error('invalid checkpoint version');
  const iv = Buffer.from(item.iv, 'base64');
  const tag = Buffer.from(item.tag, 'base64');
  if (iv.length !== 12 || tag.length !== 16) throw new Error('invalid checkpoint encryption parameters');
  const decipher = createDecipheriv('aes-256-gcm', key, iv);
  decipher.setAAD(AAD);
  decipher.setAuthTag(tag);
  const plaintext = Buffer.concat([decipher.update(Buffer.from(item.ciphertext, 'base64')), decipher.final()]);
  const value = JSON.parse(plaintext.toString('utf8'));
  validate(value);
  if (value.runId !== expectedRunId || value.attempt !== expectedAttempt) throw new Error('checkpoint source run mismatch');
  return value;
}

function run() {
  const [command, filename, runId, attempt] = process.argv.slice(2);
  const key = keyFromEnv();
  if (command === 'create') {
    const credentials = JSON.parse(process.env.UPTEST_CLOUD_CREDENTIALS || '{}');
    if (typeof credentials.api_key !== 'string' || !credentials.api_key.trim()) throw new Error('missing API key');
    const url = new URL(credentials.url || credentials.base_url || '');
    const origin = `${url.protocol}//${url.host}`;
    const allowed = (process.env.QA_VERIFIED_ORIGIN_HASHES || '').split(',').map(v => v.trim());
    const originHash = createHash('sha256').update(origin).digest('hex');
    if (url.protocol !== 'https:' || url.username || url.password || url.pathname.replace(/\/$/, '') !== '/v1' || url.search || url.hash ||
        !allowed.length || !allowed.every(v => /^[a-f0-9]{64}$/.test(v)) || !allowed.includes(originHash)) {
      throw new Error('checkpoint requires verified QA origin');
    }
    const rid = process.env.GITHUB_RUN_ID || '';
    const att = process.env.GITHUB_RUN_ATTEMPT || '';
    const value = {version: 1, repo: process.env.GITHUB_REPOSITORY, runId: rid, attempt: att,
      runName: `xp-ci-${rid}-${att}`, commit: process.env.GITHUB_SHA || '', originHash,
      credentialBinding: createHmac('sha256', key).update(credentials.api_key.trim()).digest('hex'),
      kind: process.env.QA_CHECKPOINT_KIND || ''};
    writeFileSync(filename, encryptIntent(value, key), {mode: 0o600, flag: 'wx'});
    process.stdout.write('encrypted ownership intent created\n');
  } else if (command === 'resolve') {
    const value = decryptIntent(readFileSync(filename, 'utf8'), key, runId, attempt);
    writeFileSync(`${filename}.resolved`, JSON.stringify(value), {mode: 0o600, flag: 'wx'});
    process.stdout.write('encrypted ownership intent authenticated\n');
  } else throw new Error('unknown checkpoint command');
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  try { run(); } catch { process.stderr.write('checkpoint operation failed; no ownership established\n'); process.exitCode = 1; }
}
