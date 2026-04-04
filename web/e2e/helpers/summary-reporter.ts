// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import fs from 'node:fs';
import path from 'node:path';

import type {
  FullConfig,
  FullResult,
  Reporter,
  Suite,
  TestCase,
  TestResult,
} from '@playwright/test/reporter';

/** Strip ANSI escape sequences so error messages are plain text. */
function stripAnsi(s: string): string {
  // Matches CSI sequences (colors, cursor movement) and OSC sequences
  // eslint-disable-next-line no-control-regex
  return s.replace(/\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?(?:\x07|\x1b\\)/g, '');
}

interface FailureInfo {
  title: string;
  file: string;
  line: number;
  duration: number;
  error: string;
  attachments: string[];
  consoleLogs: string | null;
}

/**
 * Writes a concise Markdown summary to test-results/summary.md after each run.
 *
 * Designed for AI assistants (Claude Code) that need to quickly understand what
 * passed, what failed, and where to look for details — without grepping stdout.
 */
class SummaryReporter implements Reporter {
  private totalTests = 0;
  private passed = 0;
  private failed = 0;
  private skipped = 0;
  private timedOut = 0;
  private failures: FailureInfo[] = [];
  private startTime = 0;
  private outputDir = '';

  onBegin(config: FullConfig, suite: Suite) {
    this.totalTests = suite.allTests().length;
    this.startTime = Date.now();
    this.outputDir = config.projects[0]?.outputDir ?? 'test-results';
  }

  onTestEnd(test: TestCase, result: TestResult) {
    switch (result.status) {
      case 'passed':
        this.passed++;
        break;
      case 'skipped':
        this.skipped++;
        break;
      case 'timedOut':
        this.timedOut++;
        this.recordFailure(test, result, 'timed out');
        break;
      case 'failed':
      case 'interrupted':
        this.failed++;
        this.recordFailure(test, result, result.status);
        break;
    }
  }

  private recordFailure(test: TestCase, result: TestResult, reason: string) {
    const rawError =
      result.error?.message ?? result.errors?.map((e) => e.message).join('\n') ?? reason;
    const errorMsg = stripAnsi(rawError);

    const attachments = result.attachments
      .filter((a) => a.path)
      .map((a) => {
        const relPath = path.relative(process.cwd(), a.path!);
        return `${a.name} (${a.contentType}): ${relPath}`;
      });

    const consoleLogs =
      result.attachments.find((a) => a.name === 'browser-console-logs')?.body?.toString() ?? null;

    this.failures.push({
      title: test.titlePath().slice(1).join(' > '),
      file: test.location.file.replace(process.cwd() + '/', ''),
      line: test.location.line,
      duration: result.duration,
      error: errorMsg,
      attachments,
      consoleLogs,
    });
  }

  onEnd(result: FullResult) {
    const elapsed = ((Date.now() - this.startTime) / 1000).toFixed(1);
    const allPassed = this.failed === 0 && this.timedOut === 0;

    const lines: string[] = [];

    lines.push(`# E2E Test Summary`);
    lines.push('');
    lines.push(`**Result: ${allPassed ? 'PASSED' : 'FAILED'}**`);
    lines.push('');
    lines.push(`| Metric | Count |`);
    lines.push(`|--------|-------|`);
    lines.push(`| Passed | ${this.passed} |`);
    lines.push(`| Failed | ${this.failed} |`);
    lines.push(`| Timed out | ${this.timedOut} |`);
    lines.push(`| Skipped | ${this.skipped} |`);
    lines.push(`| Total | ${this.totalTests} |`);
    lines.push(`| Duration | ${elapsed}s |`);

    if (this.failures.length > 0) {
      lines.push('');
      lines.push(`## Failures`);

      for (const f of this.failures) {
        lines.push('');
        lines.push(`### ${f.title}`);
        lines.push('');
        lines.push(`**Location:** ${f.file}:${f.line}`);
        lines.push(`**Duration:** ${f.duration}ms`);
        lines.push('');
        lines.push('```');
        lines.push(f.error);
        lines.push('```');

        if (f.consoleLogs) {
          lines.push('');
          lines.push('<details><summary>Browser console logs</summary>');
          lines.push('');
          lines.push('```');
          lines.push(f.consoleLogs);
          lines.push('```');
          lines.push('');
          lines.push('</details>');
        }

        if (f.attachments.length > 0) {
          lines.push('');
          lines.push('**Attachments:**');
          for (const a of f.attachments) {
            lines.push(`- ${a}`);
          }
        }
      }
    }

    if (allPassed) {
      lines.push('');
      lines.push(`All ${this.passed} tests passed in ${elapsed}s.`);
    } else {
      lines.push('');
      lines.push(`## Quick reference`);
      lines.push('');
      lines.push('Traces (time-travel debug): `npx playwright show-trace <trace.zip>`');
      lines.push('HTML report: `npx playwright show-report test-results/html`');
    }

    lines.push('');

    fs.mkdirSync(this.outputDir, { recursive: true });
    fs.writeFileSync(path.join(this.outputDir, 'summary.md'), lines.join('\n'));
  }
}

export default SummaryReporter;
