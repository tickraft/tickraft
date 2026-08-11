// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'
import type {
  TaskModel,
  TaskCreateParams,
  TaskUpdateParams,
  LogModel,
  ExecutionStats,
} from '../types/task'
import type { PageData, PageParams } from '@tickraft/core'

/** Task list query parameters (backend reads group and tags) */
export interface TaskListParams extends PageParams {
  group?: string
  /** Comma-separated tag list, e.g. "critical,nightly" */
  tags?: string
}

/** Execution log list query parameters (backend only reads page/size) */
export type LogListParams = PageParams

/** Execution stats query parameters */
export interface ExecutionStatsParams {
  /** RFC3339 timestamp for range start */
  from?: string
  /** RFC3339 timestamp for range end */
  to?: string
}

/**
 * Get task list
 */
export function getTasks(params: TaskListParams): Promise<PageData<TaskModel>> {
  return request<PageData<TaskModel>>({
    url: '/tasks',
    method: 'get',
    params,
  })
}

/**
 * Get task detail
 */
export function getTask(id: number): Promise<TaskModel> {
  return request<TaskModel>({
    url: `/tasks/${id}`,
    method: 'get',
  })
}

/**
 * Create task
 */
export function createTask(params: TaskCreateParams): Promise<TaskModel> {
  return request<TaskModel>({
    url: '/tasks',
    method: 'post',
    data: params,
  })
}

/**
 * Update task
 */
export function updateTask(id: number, params: TaskUpdateParams): Promise<TaskModel> {
  return request<TaskModel>({
    url: `/tasks/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete task
 */
export function deleteTask(id: number): Promise<void> {
  return request<void>({
    url: `/tasks/${id}`,
    method: 'delete',
  })
}

/**
 * Manually trigger task
 */
export function triggerTask(id: number): Promise<void> {
  return request<void>({
    url: `/tasks/${id}/trigger`,
    method: 'post',
  })
}

/**
 * Pause a task (removes it from the scheduling wheel, config is preserved)
 */
export function pauseTask(id: number): Promise<void> {
  return request<void>({
    url: `/tasks/${id}/pause`,
    method: 'post',
  })
}

/**
 * Resume a paused task (re-adds it to the scheduling wheel)
 */
export function resumeTask(id: number): Promise<void> {
  return request<void>({
    url: `/tasks/${id}/resume`,
    method: 'post',
  })
}

/**
 * Copy a task (clones configuration into a new task with a fresh ID)
 * @param id - The source task ID
 * @param name - Optional new task name; defaults to "<source name> (copy)"
 */
export function copyTask(id: number, name?: string): Promise<TaskModel> {
  return request<TaskModel>({
    url: `/tasks/${id}/copy`,
    method: 'post',
    data: name ? { name } : undefined,
  })
}

/**
 * Get execution stats for an optional time range.
 * Defaults to the last 24 hours when from/to are omitted.
 */
export function getExecutionStats(params?: ExecutionStatsParams): Promise<ExecutionStats> {
  return request<ExecutionStats>({
    url: '/tasks/stats',
    method: 'get',
    params,
  })
}

/**
 * Get execution log list for a specific task.
 * @param taskId - The task ID (0 for all tasks)
 */
export function getLogs(taskId: number, params: LogListParams): Promise<PageData<LogModel>> {
  return request<PageData<LogModel>>({
    url: `/tasks/${taskId}/executions`,
    method: 'get',
    params,
  })
}

/**
 * Get execution log detail
 * @param taskId - The parent task ID
 * @param execId - The execution record ID
 */
export function getLog(taskId: number, execId: number): Promise<LogModel> {
  return request<LogModel>({
    url: `/tasks/${taskId}/executions/${execId}`,
    method: 'get',
  })
}
