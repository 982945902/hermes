# Hermes Task Plan

## Phase 1 - Foundation

- [x] Create Go module and repository skeleton
- [x] Add design documentation
- [x] Add Gin server
- [x] Add MongoDB connection
- [x] Add configuration loader

## Phase 2 - Admin

- [x] Add admin login with JWT
- [x] Add admin auth middleware
- [x] Add channel CRUD APIs
- [x] Add channel test API

## Phase 3 - Relay

- [x] Add gateway API key middleware
- [x] Add `/v1/models`
- [x] Add `/v1/chat/completions`
- [x] Add model mapping
- [x] Add channel selection
- [x] Add JSON passthrough
- [x] Add SSE passthrough
- [x] Add in-memory dynamic barometer
- [x] Add runtime tiering: excellent, unstable, unavailable
- [x] Add Gaussian-race channel ranking
- [x] Add unavailable-channel shadow probes
- [x] Add identity guard request detection
- [x] Add identity guard system prompt injection
- [x] Add response and stream sanitization

## Phase 4 - Frontend

- [x] Create React 19 frontend
- [x] Add login page
- [x] Add protected admin layout
- [x] Add channel list page
- [x] Add channel editor
- [x] Add settings/status page
- [x] Add realtime barometer panel

## Phase 5 - Verification

- [ ] Verify backend build
- [ ] Verify frontend build
- [ ] Verify admin login
- [ ] Verify OpenRouter channel
- [ ] Verify Doubao Coding Plan channel
- [ ] Verify streaming response
