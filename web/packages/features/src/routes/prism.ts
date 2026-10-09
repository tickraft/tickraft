// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  {
    path: '/prism',
    component: DefaultLayout,
    children: [
      {
        path: 'record/list',
        name: 'PrismRecordList',
        component: () => import('../views/prism/record/list/List.vue'),
        meta: { title: 'prism.record.list.title' },
      },
      {
        path: 'record/detail/:id',
        name: 'PrismRecordDetail',
        component: () => import('../views/prism/record/detail/Detail.vue'),
        meta: { title: 'prism.record.detail.title', hidden: true },
      },
      {
        path: 'rule/list',
        name: 'PrismRuleList',
        component: () => import('../views/prism/rule/list/List.vue'),
        meta: { title: 'prism.rule.list.title' },
      },
      {
        path: 'templates',
        name: 'PrismTemplates',
        component: () => import('../views/prism/templates/list/List.vue'),
        meta: { title: 'prism.templates.title', feature: undefined },
      },
      {
        path: 'remediation/records',
        name: 'PrismRemediationList',
        component: () => import('../views/prism/remediation/list/List.vue'),
        meta: { title: 'prism.remediation.list.title' },
      },
      {
        path: 'remediation/rule/list',
        name: 'PrismRemediationRuleList',
        component: () => import('../views/prism/remediation/rule/list/List.vue'),
        meta: { title: 'prism.remediation.rule.list.title' },
      },
      {
        path: 'remediation/rule/edit',
        name: 'PrismRemediationRuleCreate',
        component: () => import('../views/prism/remediation/rule/edit/Edit.vue'),
        meta: { title: 'prism.remediation.rule.form.titleCreate', hidden: true },
      },
      {
        path: 'remediation/rule/edit/:id',
        name: 'PrismRemediationRuleEdit',
        component: () => import('../views/prism/remediation/rule/edit/Edit.vue'),
        meta: { title: 'prism.remediation.rule.form.titleEdit', hidden: true },
      },
      {
        path: 'channel/list',
        name: 'PrismChannelList',
        component: () => import('../views/prism/channel/list/List.vue'),
        meta: { title: 'prism.channel.list.title' },
      },
      {
        path: 'contact/list',
        name: 'PrismContactList',
        component: () => import('../views/prism/contact/list/List.vue'),
        meta: { title: 'prism.contact.list.title' },
      },
      {
        path: 'channel/form',
        name: 'PrismChannelCreate',
        component: () => import('../views/prism/channel/form/Form.vue'),
        meta: { title: 'prism.channel.form.create', hidden: true },
      },
      {
        path: 'channel/form/:id',
        name: 'PrismChannelEdit',
        component: () => import('../views/prism/channel/form/Form.vue'),
        meta: { title: 'prism.channel.form.edit', hidden: true },
      },
      {
        path: 'channel/delivery/:id',
        name: 'PrismChannelDelivery',
        component: () => import('../views/prism/channel/delivery/Delivery.vue'),
        meta: { title: 'prism.channel.delivery.title', hidden: true },
      },
      {
        path: 'rule/edit',
        name: 'PrismRuleCreate',
        component: () => import('../views/prism/rule/edit/Edit.vue'),
        meta: { title: 'prism.rule.edit.titleCreate', hidden: true },
      },
      {
        path: 'rule/edit/:id',
        name: 'PrismRuleEdit',
        component: () => import('../views/prism/rule/edit/Edit.vue'),
        meta: { title: 'prism.rule.edit.titleEdit', hidden: true },
      },
    ],
  },
]

export default routes
