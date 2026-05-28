---
title: "gRPC API Reference"
---

<a name="top"></a>

## Table of Contents

- [holomush/core/v1/core.proto](#holomush_core_v1_core-proto)
    - [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest)
    - [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse)
    - [CharacterSummary](#holomush-core-v1-CharacterSummary)
    - [CheckPlayerSessionRequest](#holomush-core-v1-CheckPlayerSessionRequest)
    - [CheckPlayerSessionResponse](#holomush-core-v1-CheckPlayerSessionResponse)
    - [ConfirmPasswordResetRequest](#holomush-core-v1-ConfirmPasswordResetRequest)
    - [ConfirmPasswordResetResponse](#holomush-core-v1-ConfirmPasswordResetResponse)
    - [ControlFrame](#holomush-core-v1-ControlFrame)
    - [CreateCharacterRequest](#holomush-core-v1-CreateCharacterRequest)
    - [CreateCharacterResponse](#holomush-core-v1-CreateCharacterResponse)
    - [CreateGuestRequest](#holomush-core-v1-CreateGuestRequest)
    - [CreateGuestResponse](#holomush-core-v1-CreateGuestResponse)
    - [CreatePlayerRequest](#holomush-core-v1-CreatePlayerRequest)
    - [CreatePlayerResponse](#holomush-core-v1-CreatePlayerResponse)
    - [DisconnectRequest](#holomush-core-v1-DisconnectRequest)
    - [DisconnectResponse](#holomush-core-v1-DisconnectResponse)
    - [EventFrame](#holomush-core-v1-EventFrame)
    - [GetCommandHistoryRequest](#holomush-core-v1-GetCommandHistoryRequest)
    - [GetCommandHistoryResponse](#holomush-core-v1-GetCommandHistoryResponse)
    - [HandleCommandRequest](#holomush-core-v1-HandleCommandRequest)
    - [HandleCommandResponse](#holomush-core-v1-HandleCommandResponse)
    - [ListCharactersRequest](#holomush-core-v1-ListCharactersRequest)
    - [ListCharactersResponse](#holomush-core-v1-ListCharactersResponse)
    - [ListFocusPresenceRequest](#holomush-core-v1-ListFocusPresenceRequest)
    - [ListFocusPresenceResponse](#holomush-core-v1-ListFocusPresenceResponse)
    - [ListPlayerSessionsRequest](#holomush-core-v1-ListPlayerSessionsRequest)
    - [ListPlayerSessionsResponse](#holomush-core-v1-ListPlayerSessionsResponse)
    - [ListSessionStreamsRequest](#holomush-core-v1-ListSessionStreamsRequest)
    - [ListSessionStreamsResponse](#holomush-core-v1-ListSessionStreamsResponse)
    - [LogoutRequest](#holomush-core-v1-LogoutRequest)
    - [LogoutResponse](#holomush-core-v1-LogoutResponse)
    - [PlayerSessionInfo](#holomush-core-v1-PlayerSessionInfo)
    - [PresenceEntry](#holomush-core-v1-PresenceEntry)
    - [QueryStreamHistoryRequest](#holomush-core-v1-QueryStreamHistoryRequest)
    - [QueryStreamHistoryResponse](#holomush-core-v1-QueryStreamHistoryResponse)
    - [RenderingMetadata](#holomush-core-v1-RenderingMetadata)
    - [RequestMeta](#holomush-core-v1-RequestMeta)
    - [RequestPasswordResetRequest](#holomush-core-v1-RequestPasswordResetRequest)
    - [RequestPasswordResetResponse](#holomush-core-v1-RequestPasswordResetResponse)
    - [ResponseMeta](#holomush-core-v1-ResponseMeta)
    - [RevokeOtherPlayerSessionsRequest](#holomush-core-v1-RevokeOtherPlayerSessionsRequest)
    - [RevokeOtherPlayerSessionsResponse](#holomush-core-v1-RevokeOtherPlayerSessionsResponse)
    - [RevokePlayerSessionRequest](#holomush-core-v1-RevokePlayerSessionRequest)
    - [RevokePlayerSessionResponse](#holomush-core-v1-RevokePlayerSessionResponse)
    - [SelectCharacterRequest](#holomush-core-v1-SelectCharacterRequest)
    - [SelectCharacterResponse](#holomush-core-v1-SelectCharacterResponse)
    - [SubscribeRequest](#holomush-core-v1-SubscribeRequest)
    - [SubscribeResponse](#holomush-core-v1-SubscribeResponse)
  
    - [ControlSignal](#holomush-core-v1-ControlSignal)
    - [EventChannel](#holomush-core-v1-EventChannel)
    - [NoPlaintextReason](#holomush-core-v1-NoPlaintextReason)
    - [PresenceContext](#holomush-core-v1-PresenceContext)
    - [PresenceState](#holomush-core-v1-PresenceState)
  
    - [CoreService](#holomush-core-v1-CoreService)
  
- [holomush/web/v1/web.proto](#holomush_web_v1_web-proto)
    - [CharacterSummary](#holomush-web-v1-CharacterSummary)
    - [ControlFrame](#holomush-web-v1-ControlFrame)
    - [DisconnectRequest](#holomush-web-v1-DisconnectRequest)
    - [DisconnectResponse](#holomush-web-v1-DisconnectResponse)
    - [GameEvent](#holomush-web-v1-GameEvent)
    - [GetCommandHistoryRequest](#holomush-web-v1-GetCommandHistoryRequest)
    - [GetCommandHistoryResponse](#holomush-web-v1-GetCommandHistoryResponse)
    - [SendCommandRequest](#holomush-web-v1-SendCommandRequest)
    - [SendCommandResponse](#holomush-web-v1-SendCommandResponse)
    - [StreamEventsRequest](#holomush-web-v1-StreamEventsRequest)
    - [StreamEventsResponse](#holomush-web-v1-StreamEventsResponse)
    - [WebAuthenticatePlayerRequest](#holomush-web-v1-WebAuthenticatePlayerRequest)
    - [WebAuthenticatePlayerResponse](#holomush-web-v1-WebAuthenticatePlayerResponse)
    - [WebCheckSessionRequest](#holomush-web-v1-WebCheckSessionRequest)
    - [WebCheckSessionResponse](#holomush-web-v1-WebCheckSessionResponse)
    - [WebConfirmPasswordResetRequest](#holomush-web-v1-WebConfirmPasswordResetRequest)
    - [WebConfirmPasswordResetResponse](#holomush-web-v1-WebConfirmPasswordResetResponse)
    - [WebContentItem](#holomush-web-v1-WebContentItem)
    - [WebContentItem.MetadataEntry](#holomush-web-v1-WebContentItem-MetadataEntry)
    - [WebCreateCharacterRequest](#holomush-web-v1-WebCreateCharacterRequest)
    - [WebCreateCharacterResponse](#holomush-web-v1-WebCreateCharacterResponse)
    - [WebCreateGuestRequest](#holomush-web-v1-WebCreateGuestRequest)
    - [WebCreateGuestResponse](#holomush-web-v1-WebCreateGuestResponse)
    - [WebCreatePlayerRequest](#holomush-web-v1-WebCreatePlayerRequest)
    - [WebCreatePlayerResponse](#holomush-web-v1-WebCreatePlayerResponse)
    - [WebGetContentRequest](#holomush-web-v1-WebGetContentRequest)
    - [WebGetContentResponse](#holomush-web-v1-WebGetContentResponse)
    - [WebListCharactersRequest](#holomush-web-v1-WebListCharactersRequest)
    - [WebListCharactersResponse](#holomush-web-v1-WebListCharactersResponse)
    - [WebListContentRequest](#holomush-web-v1-WebListContentRequest)
    - [WebListContentResponse](#holomush-web-v1-WebListContentResponse)
    - [WebListFocusPresenceRequest](#holomush-web-v1-WebListFocusPresenceRequest)
    - [WebListFocusPresenceResponse](#holomush-web-v1-WebListFocusPresenceResponse)
    - [WebListPlayerSessionsRequest](#holomush-web-v1-WebListPlayerSessionsRequest)
    - [WebListPlayerSessionsResponse](#holomush-web-v1-WebListPlayerSessionsResponse)
    - [WebListSessionStreamsRequest](#holomush-web-v1-WebListSessionStreamsRequest)
    - [WebListSessionStreamsResponse](#holomush-web-v1-WebListSessionStreamsResponse)
    - [WebLogoutRequest](#holomush-web-v1-WebLogoutRequest)
    - [WebLogoutResponse](#holomush-web-v1-WebLogoutResponse)
    - [WebPlayerSessionInfo](#holomush-web-v1-WebPlayerSessionInfo)
    - [WebPresenceEntry](#holomush-web-v1-WebPresenceEntry)
    - [WebQueryStreamHistoryRequest](#holomush-web-v1-WebQueryStreamHistoryRequest)
    - [WebQueryStreamHistoryResponse](#holomush-web-v1-WebQueryStreamHistoryResponse)
    - [WebRequestPasswordResetRequest](#holomush-web-v1-WebRequestPasswordResetRequest)
    - [WebRequestPasswordResetResponse](#holomush-web-v1-WebRequestPasswordResetResponse)
    - [WebRevokeOtherPlayerSessionsRequest](#holomush-web-v1-WebRevokeOtherPlayerSessionsRequest)
    - [WebRevokeOtherPlayerSessionsResponse](#holomush-web-v1-WebRevokeOtherPlayerSessionsResponse)
    - [WebRevokePlayerSessionRequest](#holomush-web-v1-WebRevokePlayerSessionRequest)
    - [WebRevokePlayerSessionResponse](#holomush-web-v1-WebRevokePlayerSessionResponse)
    - [WebSelectCharacterRequest](#holomush-web-v1-WebSelectCharacterRequest)
    - [WebSelectCharacterResponse](#holomush-web-v1-WebSelectCharacterResponse)
  
    - [ControlSignal](#holomush-web-v1-ControlSignal)
    - [EventChannel](#holomush-web-v1-EventChannel)
    - [WebPresenceContext](#holomush-web-v1-WebPresenceContext)
    - [WebPresenceState](#holomush-web-v1-WebPresenceState)
  
    - [WebService](#holomush-web-v1-WebService)
  
- [holomush/control/v1/control.proto](#holomush_control_v1_control-proto)
    - [ShutdownRequest](#holomush-control-v1-ShutdownRequest)
    - [ShutdownResponse](#holomush-control-v1-ShutdownResponse)
    - [StatusRequest](#holomush-control-v1-StatusRequest)
    - [StatusResponse](#holomush-control-v1-StatusResponse)
  
    - [ControlService](#holomush-control-v1-ControlService)
  
- [holomush/plugin/v1/plugin.proto](#holomush_plugin_v1_plugin-proto)
    - [AuditDecisionHint](#holomush-plugin-v1-AuditDecisionHint)
    - [AuditDecisionHint.AttributesEntry](#holomush-plugin-v1-AuditDecisionHint-AttributesEntry)
    - [CommandRequest](#holomush-plugin-v1-CommandRequest)
    - [CommandResponse](#holomush-plugin-v1-CommandResponse)
    - [EmitEvent](#holomush-plugin-v1-EmitEvent)
    - [Event](#holomush-plugin-v1-Event)
    - [FocusFailure](#holomush-plugin-v1-FocusFailure)
    - [FocusKey](#holomush-plugin-v1-FocusKey)
    - [HandleCommandRequest](#holomush-plugin-v1-HandleCommandRequest)
    - [HandleCommandResponse](#holomush-plugin-v1-HandleCommandResponse)
    - [HandleEventRequest](#holomush-plugin-v1-HandleEventRequest)
    - [HandleEventResponse](#holomush-plugin-v1-HandleEventResponse)
    - [InitRequest](#holomush-plugin-v1-InitRequest)
    - [InitResponse](#holomush-plugin-v1-InitResponse)
    - [PluginHostServiceAddSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceAddSessionStreamRequest)
    - [PluginHostServiceAddSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceAddSessionStreamResponse)
    - [PluginHostServiceAutoFocusOnJoinRequest](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinRequest)
    - [PluginHostServiceAutoFocusOnJoinResponse](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinResponse)
    - [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest)
    - [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse)
    - [PluginHostServiceEvaluateRequest](#holomush-plugin-v1-PluginHostServiceEvaluateRequest)
    - [PluginHostServiceEvaluateResponse](#holomush-plugin-v1-PluginHostServiceEvaluateResponse)
    - [PluginHostServiceIsAnyConnFocusedRequest](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedRequest)
    - [PluginHostServiceIsAnyConnFocusedResponse](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedResponse)
    - [PluginHostServiceJoinFocusRequest](#holomush-plugin-v1-PluginHostServiceJoinFocusRequest)
    - [PluginHostServiceJoinFocusResponse](#holomush-plugin-v1-PluginHostServiceJoinFocusResponse)
    - [PluginHostServiceKVDeleteRequest](#holomush-plugin-v1-PluginHostServiceKVDeleteRequest)
    - [PluginHostServiceKVDeleteResponse](#holomush-plugin-v1-PluginHostServiceKVDeleteResponse)
    - [PluginHostServiceKVGetRequest](#holomush-plugin-v1-PluginHostServiceKVGetRequest)
    - [PluginHostServiceKVGetResponse](#holomush-plugin-v1-PluginHostServiceKVGetResponse)
    - [PluginHostServiceKVSetRequest](#holomush-plugin-v1-PluginHostServiceKVSetRequest)
    - [PluginHostServiceKVSetResponse](#holomush-plugin-v1-PluginHostServiceKVSetResponse)
    - [PluginHostServiceLeaveFocusByTargetRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetRequest)
    - [PluginHostServiceLeaveFocusByTargetResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetResponse)
    - [PluginHostServiceLeaveFocusRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusRequest)
    - [PluginHostServiceLeaveFocusResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusResponse)
    - [PluginHostServiceLogRequest](#holomush-plugin-v1-PluginHostServiceLogRequest)
    - [PluginHostServiceLogResponse](#holomush-plugin-v1-PluginHostServiceLogResponse)
    - [PluginHostServicePresentFocusRequest](#holomush-plugin-v1-PluginHostServicePresentFocusRequest)
    - [PluginHostServicePresentFocusResponse](#holomush-plugin-v1-PluginHostServicePresentFocusResponse)
    - [PluginHostServiceQueryStreamHistoryRequest](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryRequest)
    - [PluginHostServiceQueryStreamHistoryResponse](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryResponse)
    - [PluginHostServiceRemoveSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamRequest)
    - [PluginHostServiceRemoveSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamResponse)
    - [PluginHostServiceRequestEmitTokenRequest](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenRequest)
    - [PluginHostServiceRequestEmitTokenResponse](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenResponse)
    - [PluginHostServiceSetConnectionFocusRequest](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusRequest)
    - [PluginHostServiceSetConnectionFocusResponse](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusResponse)
    - [QuerySessionStreamsRequest](#holomush-plugin-v1-QuerySessionStreamsRequest)
    - [QuerySessionStreamsResponse](#holomush-plugin-v1-QuerySessionStreamsResponse)
    - [ServiceConfig](#holomush-plugin-v1-ServiceConfig)
    - [ServiceConfig.PluginConfigEntry](#holomush-plugin-v1-ServiceConfig-PluginConfigEntry)
    - [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry)
  
    - [AuditEffect](#holomush-plugin-v1-AuditEffect)
    - [CommandStatus](#holomush-plugin-v1-CommandStatus)
    - [FocusFailureReason](#holomush-plugin-v1-FocusFailureReason)
    - [FocusKind](#holomush-plugin-v1-FocusKind)
    - [StreamReplayMode](#holomush-plugin-v1-StreamReplayMode)
  
    - [PluginHostService](#holomush-plugin-v1-PluginHostService)
    - [PluginService](#holomush-plugin-v1-PluginService)
  
- [holomush/plugin/v1/hostfunc.proto](#holomush_plugin_v1_hostfunc-proto)
    - [AddSessionStreamRequest](#holomush-plugin-v1-AddSessionStreamRequest)
    - [AddSessionStreamResponse](#holomush-plugin-v1-AddSessionStreamResponse)
    - [CharacterInfo](#holomush-plugin-v1-CharacterInfo)
    - [CommandHelpInfo](#holomush-plugin-v1-CommandHelpInfo)
    - [CommandInfo](#holomush-plugin-v1-CommandInfo)
    - [EmitEventRequest](#holomush-plugin-v1-EmitEventRequest)
    - [EmitEventResponse](#holomush-plugin-v1-EmitEventResponse)
    - [GetCommandHelpRequest](#holomush-plugin-v1-GetCommandHelpRequest)
    - [GetCommandHelpResponse](#holomush-plugin-v1-GetCommandHelpResponse)
    - [KVDeleteRequest](#holomush-plugin-v1-KVDeleteRequest)
    - [KVDeleteResponse](#holomush-plugin-v1-KVDeleteResponse)
    - [KVGetRequest](#holomush-plugin-v1-KVGetRequest)
    - [KVGetResponse](#holomush-plugin-v1-KVGetResponse)
    - [KVSetRequest](#holomush-plugin-v1-KVSetRequest)
    - [KVSetResponse](#holomush-plugin-v1-KVSetResponse)
    - [ListCommandsRequest](#holomush-plugin-v1-ListCommandsRequest)
    - [ListCommandsResponse](#holomush-plugin-v1-ListCommandsResponse)
    - [LocationInfo](#holomush-plugin-v1-LocationInfo)
    - [LogRequest](#holomush-plugin-v1-LogRequest)
    - [LogRequest.FieldsEntry](#holomush-plugin-v1-LogRequest-FieldsEntry)
    - [LogResponse](#holomush-plugin-v1-LogResponse)
    - [QueryCharacterRequest](#holomush-plugin-v1-QueryCharacterRequest)
    - [QueryCharacterResponse](#holomush-plugin-v1-QueryCharacterResponse)
    - [QueryLocationCharactersRequest](#holomush-plugin-v1-QueryLocationCharactersRequest)
    - [QueryLocationCharactersResponse](#holomush-plugin-v1-QueryLocationCharactersResponse)
    - [QueryLocationRequest](#holomush-plugin-v1-QueryLocationRequest)
    - [QueryLocationResponse](#holomush-plugin-v1-QueryLocationResponse)
    - [RemoveSessionStreamRequest](#holomush-plugin-v1-RemoveSessionStreamRequest)
    - [RemoveSessionStreamResponse](#holomush-plugin-v1-RemoveSessionStreamResponse)
  
    - [LogLevel](#holomush-plugin-v1-LogLevel)
  
    - [HostFunctionsService](#holomush-plugin-v1-HostFunctionsService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="holomush_core_v1_core-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/core/v1/core.proto



<a name="holomush-core-v1-AuthenticatePlayerRequest"></a>

### AuthenticatePlayerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| captcha_token | [string](#string) |  |  |
| remember_me | [bool](#bool) |  |  |






<a name="holomush-core-v1-AuthenticatePlayerResponse"></a>

### AuthenticatePlayerResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| player_session_token | [string](#string) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |
| session_ttl_seconds | [int64](#int64) |  | Session TTL in seconds. Used by the web gateway to set cookie MaxAge so the cookie expires when the underlying session expires (prevents stale cookies outliving 2h guest sessions). |






<a name="holomush-core-v1-CharacterSummary"></a>

### CharacterSummary



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| has_active_session | [bool](#bool) |  |  |
| session_status | [string](#string) |  |  |
| last_location | [string](#string) |  |  |
| last_played_at | [int64](#int64) |  |  |






<a name="holomush-core-v1-CheckPlayerSessionRequest"></a>

### CheckPlayerSessionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-CheckPlayerSessionResponse"></a>

### CheckPlayerSessionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  |  |
| player_id | [string](#string) |  | NEW (additive on the success path; failure path still returns nil, err so these fields are absent on PLAYER_SESSION_NOT_FOUND / PLAYER_SESSION_EXPIRED — preserves the enumeration-safety contract documented at internal/auth/session_ownership.go:18-20). |
| is_guest | [bool](#bool) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |






<a name="holomush-core-v1-ConfirmPasswordResetRequest"></a>

### ConfirmPasswordResetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| new_password | [string](#string) |  |  |






<a name="holomush-core-v1-ConfirmPasswordResetResponse"></a>

### ConfirmPasswordResetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-core-v1-ControlFrame"></a>

### ControlFrame



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-core-v1-ControlSignal) |  |  |
| message | [string](#string) |  |  |
| attach_moment_ms | [int64](#int64) |  | attach_moment_ms is the server&#39;s wall-clock epoch-ms at the moment the Subscribe handler attached its durable consumer. Carried ONLY on CONTROL_SIGNAL_REPLAY_COMPLETE; clients reading other signals MUST ignore this field. The client passes this value as not_after_ms on subsequent backfill (WebQueryStreamHistory) calls so backfill returns ONLY events with timestamp &lt;= attach_moment_ms — eliminating the race where a post-attach event could appear both as a dimmed backfill row and a live Subscribe delivery (holomush-iu8j; fujt Fix B). 0 on legacy servers; clients MUST treat 0 as &#34;no upper bound&#34; (back-compat). |






<a name="holomush-core-v1-CreateCharacterRequest"></a>

### CreateCharacterRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |
| character_name | [string](#string) |  |  |






<a name="holomush-core-v1-CreateCharacterResponse"></a>

### CreateCharacterResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-core-v1-CreateGuestRequest"></a>

### CreateGuestRequest







<a name="holomush-core-v1-CreateGuestResponse"></a>

### CreateGuestResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| player_session_token | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |
| session_ttl_seconds | [int64](#int64) |  | Session TTL in seconds (see AuthenticatePlayerResponse). For guest sessions this is 2h, not the 24h regular-player TTL. |






<a name="holomush-core-v1-CreatePlayerRequest"></a>

### CreatePlayerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| email | [string](#string) |  |  |
| captcha_token | [string](#string) |  |  |






<a name="holomush-core-v1-CreatePlayerResponse"></a>

### CreatePlayerResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| player_session_token | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| error_message | [string](#string) |  |  |
| session_ttl_seconds | [int64](#int64) |  | Session TTL in seconds (see AuthenticatePlayerResponse). |






<a name="holomush-core-v1-DisconnectRequest"></a>

### DisconnectRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| connection_id | [string](#string) |  | optional: remove specific connection |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. Must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |






<a name="holomush-core-v1-DisconnectResponse"></a>

### DisconnectResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |






<a name="holomush-core-v1-EventFrame"></a>

### EventFrame



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| stream | [string](#string) |  |  |
| type | [string](#string) |  |  |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |
| actor_type | [string](#string) |  |  |
| actor_id | [string](#string) |  |  |
| payload | [bytes](#bytes) |  |  |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor for this event. Populated by the server on QueryStreamHistory responses and Subscribe deliveries so clients can resume without re-delivering events they already processed. |
| rendering | [RenderingMetadata](#holomush-core-v1-RenderingMetadata) |  | Rendering metadata — cleartext band, populated by RenderingPublisher at emit time. MUST be present on every frame produced by this server (INV-GW-2). Gateway treats absence as a contract violation (drops &#43; metric &#43; log per INV-GW-5). |
| metadata_only | [bool](#bool) |  | metadata_only flags a delivery whose plaintext was withheld by the host&#39;s AuthGuard (Phase 3b decrypt path). When true, payload is empty bytes and the recipient was either not in the DEK&#39;s participant set, lacked the requisite plugin manifest declaration / ABAC grant, or hit the audit-emit backpressure throttle. metadata_only=false on every legitimate delivery (including legitimately-empty-payload events like a presence event with no content).

Set by the host&#39;s Subscribe / QueryStreamHistory handler at fan-out time (Phase 3b grounding doc Decision 4). NEVER set by emitters; NEVER persisted to events_audit (storage rows always carry the sender&#39;s payload, ciphertext or cleartext). |
| no_plaintext_reason | [NoPlaintextReason](#holomush-core-v1-NoPlaintextReason) |  | no_plaintext_reason classifies why metadata_only=true was stamped. UNSPECIFIED on metadata_only=false deliveries; one of the typed reasons when metadata_only=true. Added for holomush-ojw1.6. |






<a name="holomush-core-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. Must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |






<a name="holomush-core-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |
| commands | [string](#string) | repeated |  |
| error | [string](#string) |  |  |






<a name="holomush-core-v1-HandleCommandRequest"></a>

### HandleCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| command | [string](#string) |  |  |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. Must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |
| connection_id | [string](#string) |  | connection_id is the ULID of the originating gateway connection (Phase 5). Populated by telnet and web gateways; empty for non-gateway callers. The server uses this to route scene-focus autofocus to the correct connection (T20-T23). Empty string is accepted (zero value). |






<a name="holomush-core-v1-HandleCommandResponse"></a>

### HandleCommandResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |
| error | [string](#string) |  |  |






<a name="holomush-core-v1-ListCharactersRequest"></a>

### ListCharactersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-ListCharactersResponse"></a>

### ListCharactersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |






<a name="holomush-core-v1-ListFocusPresenceRequest"></a>

### ListFocusPresenceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| player_session_token | [string](#string) |  |  |
| session_id | [string](#string) |  |  |






<a name="holomush-core-v1-ListFocusPresenceResponse"></a>

### ListFocusPresenceResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| context | [PresenceContext](#holomush-core-v1-PresenceContext) |  |  |
| context_id | [string](#string) |  | LOCATION → location_id; SCENE → scene_id (future) |
| entries | [PresenceEntry](#holomush-core-v1-PresenceEntry) | repeated |  |






<a name="holomush-core-v1-ListPlayerSessionsRequest"></a>

### ListPlayerSessionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-ListPlayerSessionsResponse"></a>

### ListPlayerSessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [PlayerSessionInfo](#holomush-core-v1-PlayerSessionInfo) | repeated |  |






<a name="holomush-core-v1-ListSessionStreamsRequest"></a>

### ListSessionStreamsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-ListSessionStreamsResponse"></a>

### ListSessionStreamsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| streams | [string](#string) | repeated |  |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |






<a name="holomush-core-v1-LogoutRequest"></a>

### LogoutRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-LogoutResponse"></a>

### LogoutResponse







<a name="holomush-core-v1-PlayerSessionInfo"></a>

### PlayerSessionInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the PlayerSession.id (ULID). Safe to show the user - this is a resource handle, not a secret. Used as the target_session_id argument to RevokePlayerSession. |
| created_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |
| last_active | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | last_active is sourced from player_sessions.updated_at, which is bumped whenever the session is refreshed. |
| user_agent | [string](#string) |  |  |
| ip_address | [string](#string) |  |  |
| is_current | [bool](#bool) |  | is_current is true for exactly the PlayerSession that made the ListPlayerSessions request - supports &#34;This device&#34; UX. |






<a name="holomush-core-v1-PresenceEntry"></a>

### PresenceEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| state | [PresenceState](#holomush-core-v1-PresenceState) |  | Deliberately NO arrived_at_ms — see spec §D-4 (no duration-of-presence leak). |






<a name="holomush-core-v1-QueryStreamHistoryRequest"></a>

### QueryStreamHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| stream | [string](#string) |  |  |
| count | [int32](#int32) |  | page size; 0 = default (150), max 500, negative rejected |
| not_before_ms | [int64](#int64) |  | epoch ms time floor; 0 = no lower bound |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous QueryStreamHistoryResponse. Events older than the cursor position are returned. Empty = start from latest. |
| not_after_ms | [int64](#int64) |  | not_after_ms is the epoch-ms time ceiling. 0 = no upper bound (back-compat). INCLUSIVE: events with timestamp == not_after_ms are returned. Used by the web client&#39;s connect-time backfill to bound history to events that existed before the Subscribe stream attached, eliminating the connect-time race where a user-emitted event could appear both as a dimmed backfill row and a live Subscribe delivery (holomush-iu8j; holomush-fujt Fix B). |






<a name="holomush-core-v1-QueryStreamHistoryResponse"></a>

### QueryStreamHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| events | [EventFrame](#holomush-core-v1-EventFrame) | repeated |  |
| has_more | [bool](#bool) |  |  |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next page. Empty if has_more is false. |






<a name="holomush-core-v1-RenderingMetadata"></a>

### RenderingMetadata
RenderingMetadata carries cleartext rendering instructions for an event.
Populated by RenderingPublisher.Publish at emit time from the verb
registry. See docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| category | [string](#string) |  | Category drives client-side renderer routing. |
| format | [string](#string) |  | Format drives within-category presentation. |
| label | [string](#string) |  | Label provides type-specific display text. Required when format == &#34;speech&#34;. |
| display_target | [EventChannel](#holomush-core-v1-EventChannel) |  | DisplayTarget routes the event to TERMINAL, STATE, or BOTH on the client. |
| source_plugin | [string](#string) |  | SourcePlugin names the plugin that owns this event type, or &#34;builtin&#34; for host-owned types. Recorded for historical/audit fidelity. |
| source_plugin_version | [string](#string) |  | SourcePluginVersion is the manifest&#39;s version field, or &#34;host-&lt;binary version&gt;&#34; for builtins. Recorded for historical/audit fidelity. |






<a name="holomush-core-v1-RequestMeta"></a>

### RequestMeta
RequestMeta contains metadata for request correlation and debugging.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | ULID for log correlation |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |






<a name="holomush-core-v1-RequestPasswordResetRequest"></a>

### RequestPasswordResetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |






<a name="holomush-core-v1-RequestPasswordResetResponse"></a>

### RequestPasswordResetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |






<a name="holomush-core-v1-ResponseMeta"></a>

### ResponseMeta
ResponseMeta contains metadata echoed back from requests.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | Echoed from request |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |






<a name="holomush-core-v1-RevokeOtherPlayerSessionsRequest"></a>

### RevokeOtherPlayerSessionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |






<a name="holomush-core-v1-RevokeOtherPlayerSessionsResponse"></a>

### RevokeOtherPlayerSessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| revoked_count | [int32](#int32) |  |  |






<a name="holomush-core-v1-RevokePlayerSessionRequest"></a>

### RevokePlayerSessionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |
| target_session_id | [string](#string) |  | target_session_id is the PlayerSession.id (ULID) to revoke - NOT the game session_id. Different concept. |






<a name="holomush-core-v1-RevokePlayerSessionResponse"></a>

### RevokePlayerSessionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-core-v1-SelectCharacterRequest"></a>

### SelectCharacterRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |
| character_id | [string](#string) |  |  |






<a name="holomush-core-v1-SelectCharacterResponse"></a>

### SelectCharacterResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| session_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| reattached | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-core-v1-SubscribeRequest"></a>

### SubscribeRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. |
| connection_id | [string](#string) |  | connection_id identifies this specific client attachment. Gateway generates a fresh ULID per stream. Required so core can register and deregister connections atomically with the stream lifecycle. |
| client_type | [string](#string) |  | client_type describes the connecting client for observability and routing: &#34;terminal&#34;, &#34;telnet&#34;, or future client types. |






<a name="holomush-core-v1-SubscribeResponse"></a>

### SubscribeResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [EventFrame](#holomush-core-v1-EventFrame) |  |  |
| control | [ControlFrame](#holomush-core-v1-ControlFrame) |  |  |





 


<a name="holomush-core-v1-ControlSignal"></a>

### ControlSignal


| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 |  |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 |  |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 |  |



<a name="holomush-core-v1-EventChannel"></a>

### EventChannel
EventChannel identifies the destination channel for event delivery.
This is the canonical internal definition; webv1.EventChannel is kept
in lockstep for the web wire format (INV-GW-16).

| Name | Number | Description |
| ---- | ------ | ----------- |
| EVENT_CHANNEL_UNSPECIFIED | 0 |  |
| EVENT_CHANNEL_TERMINAL | 1 |  |
| EVENT_CHANNEL_STATE | 2 |  |
| EVENT_CHANNEL_BOTH | 3 |  |
| EVENT_CHANNEL_AUDIT_ONLY | 4 | EVENT_CHANNEL_AUDIT_ONLY tags host-emit security/audit events that MUST persist to events_audit but MUST NOT be delivered to client surfaces (telnet, web). The gRPC Subscribe handler drops these before send; the audit projection persists them like any other event. Used by crypto.totp_*, crypto.policy_set, and similar host-emitted audit types. |



<a name="holomush-core-v1-NoPlaintextReason"></a>

### NoPlaintextReason
NoPlaintextReason enumerates causes for metadata_only=true so clients
can distinguish e.g. a destroyed/stale DEK from an authorization denial
or backpressure-driven withholding (holomush-ojw1.6).

UNSPECIFIED is the zero value and MUST hold when metadata_only=false.
Clients seeing UNSPECIFIED with metadata_only=true MUST treat it as a
contract violation (host stamped without classifying).

| Name | Number | Description |
| ---- | ------ | ----------- |
| NO_PLAINTEXT_REASON_UNSPECIFIED | 0 |  |
| NO_PLAINTEXT_REASON_AUTHGUARD_DENY | 1 | Recipient was not in the DEK&#39;s participant set or lacked the requisite plugin manifest declaration / ABAC grant. Phase 3b AuthGuard deny. |
| NO_PLAINTEXT_REASON_STALE_DEK | 2 | Hot AND cold tier DEKs both indecipherable. Production-real post sub-epic E rekey &#43; DEK destruction. INV-E21 double miss. |
| NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL | 3 | Plugin audit emit backpressure (queue full). Host-side TOCTOU. |
| NO_PLAINTEXT_REASON_DEK_MISSING | 4 | Cold-tier audit row has no dek_ref (DEK reference column missing or NULL). Stamped exclusively by F&#39;s operator-read classifier (INV-F16). |
| NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS | 5 | Cold-tier audit row references a DEK whose column set does not match the event&#39;s AAD declaration. Stamped exclusively by F&#39;s classifier. |
| NO_PLAINTEXT_REASON_INTERNAL | 6 | Catch-all for unexpected decrypt failures not covered by the specific cases above. Stamped exclusively by F&#39;s classifier. |
| NO_PLAINTEXT_REASON_DOWNGRADE_REFUSED | 7 | Phase 7 PluginDowngradeFence layer (1) refusal — the host&#39;s read-side fence rejected the row before decrypt either because the type is in the always-sensitive manifest set and the plugin returned identity codec (INV-P7-7), or because the dek_ref is unknown / absent for a non-identity codec (INV-P7-15). Original event_id is preserved; payload is empty per master INV-26. |



<a name="holomush-core-v1-PresenceContext"></a>

### PresenceContext


| Name | Number | Description |
| ---- | ------ | ----------- |
| PRESENCE_CONTEXT_UNSPECIFIED | 0 |  |
| PRESENCE_CONTEXT_LOCATION | 1 |  |
| PRESENCE_CONTEXT_SCENE | 2 | wire-reserved; resolver in follow-up bead |



<a name="holomush-core-v1-PresenceState"></a>

### PresenceState


| Name | Number | Description |
| ---- | ------ | ----------- |
| PRESENCE_STATE_UNSPECIFIED | 0 |  |
| PRESENCE_STATE_ACTIVE | 1 |  |
| PRESENCE_STATE_DETACHED | 2 | emitted by future scene resolver |
| PRESENCE_STATE_INACTIVE | 3 | emitted by future scene resolver |


 

 


<a name="holomush-core-v1-CoreService"></a>

### CoreService
CoreService is the main game service.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| HandleCommand | [HandleCommandRequest](#holomush-core-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-core-v1-HandleCommandResponse) | HandleCommand processes a game command. |
| Subscribe | [SubscribeRequest](#holomush-core-v1-SubscribeRequest) | [SubscribeResponse](#holomush-core-v1-SubscribeResponse) stream | Subscribe opens a stream of events for the session. |
| Disconnect | [DisconnectRequest](#holomush-core-v1-DisconnectRequest) | [DisconnectResponse](#holomush-core-v1-DisconnectResponse) | Disconnect ends a session. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-core-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-core-v1-GetCommandHistoryResponse) | GetCommandHistory retrieves command history for a session. |
| AuthenticatePlayer | [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest) | [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse) | Two-phase login: authenticate player credentials. |
| SelectCharacter | [SelectCharacterRequest](#holomush-core-v1-SelectCharacterRequest) | [SelectCharacterResponse](#holomush-core-v1-SelectCharacterResponse) | Two-phase login: select a character, creating or reattaching a game session. |
| CreatePlayer | [CreatePlayerRequest](#holomush-core-v1-CreatePlayerRequest) | [CreatePlayerResponse](#holomush-core-v1-CreatePlayerResponse) | Create a new player account. |
| CreateGuest | [CreateGuestRequest](#holomush-core-v1-CreateGuestRequest) | [CreateGuestResponse](#holomush-core-v1-CreateGuestResponse) | Create an ephemeral guest player and character. |
| CreateCharacter | [CreateCharacterRequest](#holomush-core-v1-CreateCharacterRequest) | [CreateCharacterResponse](#holomush-core-v1-CreateCharacterResponse) | Create a new character for an authenticated player. |
| ListCharacters | [ListCharactersRequest](#holomush-core-v1-ListCharactersRequest) | [ListCharactersResponse](#holomush-core-v1-ListCharactersResponse) | List characters for an authenticated player. |
| RequestPasswordReset | [RequestPasswordResetRequest](#holomush-core-v1-RequestPasswordResetRequest) | [RequestPasswordResetResponse](#holomush-core-v1-RequestPasswordResetResponse) | Request a password reset (email stubbed). |
| ConfirmPasswordReset | [ConfirmPasswordResetRequest](#holomush-core-v1-ConfirmPasswordResetRequest) | [ConfirmPasswordResetResponse](#holomush-core-v1-ConfirmPasswordResetResponse) | Confirm a password reset with token. |
| Logout | [LogoutRequest](#holomush-core-v1-LogoutRequest) | [LogoutResponse](#holomush-core-v1-LogoutResponse) | End a player session. |
| CheckPlayerSession | [CheckPlayerSessionRequest](#holomush-core-v1-CheckPlayerSessionRequest) | [CheckPlayerSessionResponse](#holomush-core-v1-CheckPlayerSessionResponse) | Validate a player session token. Used by web gateway for cookie-based auth checks. |
| ListPlayerSessions | [ListPlayerSessionsRequest](#holomush-core-v1-ListPlayerSessionsRequest) | [ListPlayerSessionsResponse](#holomush-core-v1-ListPlayerSessionsResponse) | ListPlayerSessions returns the caller&#39;s active PlayerSessions (the rows of player_sessions for the caller&#39;s player_id). Tokens are not returned — only metadata useful for user-visible session management (&#34;you are signed in on these devices&#34;). |
| RevokePlayerSession | [RevokePlayerSessionRequest](#holomush-core-v1-RevokePlayerSessionRequest) | [RevokePlayerSessionResponse](#holomush-core-v1-RevokePlayerSessionResponse) | RevokePlayerSession deletes a specific PlayerSession. Ownership is verified — a player cannot revoke another player&#39;s sessions. |
| RevokeOtherPlayerSessions | [RevokeOtherPlayerSessionsRequest](#holomush-core-v1-RevokeOtherPlayerSessionsRequest) | [RevokeOtherPlayerSessionsResponse](#holomush-core-v1-RevokeOtherPlayerSessionsResponse) | RevokeOtherPlayerSessions deletes all PlayerSessions for the caller except the current one. Convenience bulk operation equivalent to listing and calling RevokePlayerSession for each. |
| QueryStreamHistory | [QueryStreamHistoryRequest](#holomush-core-v1-QueryStreamHistoryRequest) | [QueryStreamHistoryResponse](#holomush-core-v1-QueryStreamHistoryResponse) | QueryStreamHistory reads paginated event history from a stream. Two-layer authorization: membership gate (I-17) for private streams, ABAC policy evaluation for public streams. Pure read — does not mutate session cursors (invariant I-13). |
| ListSessionStreams | [ListSessionStreamsRequest](#holomush-core-v1-ListSessionStreamsRequest) | [ListSessionStreamsResponse](#holomush-core-v1-ListSessionStreamsResponse) | ListSessionStreams returns the set of streams the session is currently subscribed to, derived from focusCoordinator.RestoreFocus. Used by web clients to enumerate streams for backfill on reload. Pure read. |
| ListFocusPresence | [ListFocusPresenceRequest](#holomush-core-v1-ListFocusPresenceRequest) | [ListFocusPresenceResponse](#holomush-core-v1-ListFocusPresenceResponse) | ListFocusPresence returns the presence snapshot for the session&#39;s current focus context (location or scene). Pure read — no session mutation. |

 



<a name="holomush_web_v1_web-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/web/v1/web.proto



<a name="holomush-web-v1-CharacterSummary"></a>

### CharacterSummary



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| has_active_session | [bool](#bool) |  |  |
| session_status | [string](#string) |  |  |
| last_location | [string](#string) |  |  |
| last_played_at | [int64](#int64) |  |  |






<a name="holomush-web-v1-ControlFrame"></a>

### ControlFrame



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-web-v1-ControlSignal) |  |  |
| message | [string](#string) |  |  |
| connection_id | [string](#string) |  | connection_id is populated on the first ControlFrame after a successful StreamEvents open so the client can include it in subsequent SendCommand requests. Per-stream identity for multi-tab routing (Phase 5 scene-focus autofocus). Empty on non-open frames. |
| attach_moment_ms | [int64](#int64) |  | attach_moment_ms is the server&#39;s wall-clock epoch-ms at the moment the Subscribe handler attached its durable consumer. Carried ONLY on CONTROL_SIGNAL_REPLAY_COMPLETE; clients reading other signals MUST ignore this field. The client passes this value as not_after_ms on subsequent backfill (WebQueryStreamHistory) calls so backfill returns ONLY events with timestamp &lt;= attach_moment_ms — eliminating the connect-time replay/backfill race where a post-attach event could appear both as a dimmed backfill row and a live Subscribe delivery (holomush-iu8j; fujt Fix B). 0 on legacy/pre-iu8j servers; clients MUST treat 0 as &#34;no upper bound&#34; (back-compat). |






<a name="holomush-web-v1-DisconnectRequest"></a>

### DisconnectRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="holomush-web-v1-DisconnectResponse"></a>

### DisconnectResponse







<a name="holomush-web-v1-GameEvent"></a>

### GameEvent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| category | [string](#string) |  |  |
| format | [string](#string) |  |  |
| display_target | [EventChannel](#holomush-web-v1-EventChannel) |  |  |
| timestamp | [int64](#int64) |  |  |
| actor | [string](#string) |  |  |
| text | [string](#string) |  |  |
| metadata | [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct) |  |  |
| event_id | [string](#string) |  | ULID; populated from corev1.EventFrame.id |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor for this event. Mirrors corev1.EventFrame.cursor for reconnect-with-backfill support. |
| actor_id | [string](#string) |  | actor_id is the ULID identity of the actor (character/plugin/system), forwarded from corev1.EventFrame.actor_id. Distinct from `actor` above which is the display name extracted from the JSON payload — name is for rendering; actor_id is for stable cross-event keying (e.g., presence list dedup, self-message detection, ABAC correlation). Empty for events without a typed actor. Added by holomush-5b2j.13. |






<a name="holomush-web-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="holomush-web-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [string](#string) | repeated |  |






<a name="holomush-web-v1-SendCommandRequest"></a>

### SendCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| text | [string](#string) |  |  |
| connection_id | [string](#string) |  | connection_id identifies the originating StreamEvents stream for per-connection command routing (Phase 5 scene-focus autofocus). Clients set this from the connection_id they receive in the STREAM_OPENED ControlFrame after StreamEvents opens. Empty means &#34;no specific connection origin&#34; (scripted / admin paths). |






<a name="holomush-web-v1-SendCommandResponse"></a>

### SendCommandResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| output | [string](#string) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-web-v1-StreamEventsRequest"></a>

### StreamEventsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="holomush-web-v1-StreamEventsResponse"></a>

### StreamEventsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [GameEvent](#holomush-web-v1-GameEvent) |  |  |
| control | [ControlFrame](#holomush-web-v1-ControlFrame) |  |  |






<a name="holomush-web-v1-WebAuthenticatePlayerRequest"></a>

### WebAuthenticatePlayerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| remember_me | [bool](#bool) |  |  |






<a name="holomush-web-v1-WebAuthenticatePlayerResponse"></a>

### WebAuthenticatePlayerResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |
| error_code | [string](#string) |  | NEW: machine-readable error code. Values: &#34;&#34; on success, &#34;ALREADY_AUTHENTICATED&#34; when the cookie-collision gate fires, others reserved for future use. |
| current_player_name | [string](#string) |  | NEW: populated only when error_code = &#34;ALREADY_AUTHENTICATED&#34;. Holds the existing player&#39;s display name so the client renders the right &#34;you are already signed in as X&#34; UI without a second round trip. |






<a name="holomush-web-v1-WebCheckSessionRequest"></a>

### WebCheckSessionRequest







<a name="holomush-web-v1-WebCheckSessionResponse"></a>

### WebCheckSessionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  |  |
| player_id | [string](#string) |  | NEW (additive on the success path; failure path still returns connect.CodeUnauthenticated so web/src/routes/(authed)/&#43;layout.ts:18-25 continues to redirect on throw — no contract break). |
| is_guest | [bool](#bool) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |






<a name="holomush-web-v1-WebConfirmPasswordResetRequest"></a>

### WebConfirmPasswordResetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| new_password | [string](#string) |  |  |






<a name="holomush-web-v1-WebConfirmPasswordResetResponse"></a>

### WebConfirmPasswordResetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-web-v1-WebContentItem"></a>

### WebContentItem



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| content_type | [string](#string) |  |  |
| body | [bytes](#bytes) |  |  |
| metadata | [WebContentItem.MetadataEntry](#holomush-web-v1-WebContentItem-MetadataEntry) | repeated |  |






<a name="holomush-web-v1-WebContentItem-MetadataEntry"></a>

### WebContentItem.MetadataEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreateCharacterRequest"></a>

### WebCreateCharacterRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_name | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreateCharacterResponse"></a>

### WebCreateCharacterResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreateGuestRequest"></a>

### WebCreateGuestRequest







<a name="holomush-web-v1-WebCreateGuestResponse"></a>

### WebCreateGuestResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |
| error_code | [string](#string) |  | NEW: see WebAuthenticatePlayerResponse for semantics. |
| current_player_name | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreatePlayerRequest"></a>

### WebCreatePlayerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| email | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreatePlayerResponse"></a>

### WebCreatePlayerResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| error_message | [string](#string) |  |  |
| error_code | [string](#string) |  | NEW: see WebAuthenticatePlayerResponse for semantics. |
| current_player_name | [string](#string) |  |  |






<a name="holomush-web-v1-WebGetContentRequest"></a>

### WebGetContentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |






<a name="holomush-web-v1-WebGetContentResponse"></a>

### WebGetContentResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| item | [WebContentItem](#holomush-web-v1-WebContentItem) |  |  |






<a name="holomush-web-v1-WebListCharactersRequest"></a>

### WebListCharactersRequest







<a name="holomush-web-v1-WebListCharactersResponse"></a>

### WebListCharactersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |






<a name="holomush-web-v1-WebListContentRequest"></a>

### WebListContentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| prefix | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| cursor | [string](#string) |  |  |






<a name="holomush-web-v1-WebListContentResponse"></a>

### WebListContentResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [WebContentItem](#holomush-web-v1-WebContentItem) | repeated |  |
| next_cursor | [string](#string) |  |  |






<a name="holomush-web-v1-WebListFocusPresenceRequest"></a>

### WebListFocusPresenceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="holomush-web-v1-WebListFocusPresenceResponse"></a>

### WebListFocusPresenceResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| context | [WebPresenceContext](#holomush-web-v1-WebPresenceContext) |  |  |
| context_id | [string](#string) |  |  |
| entries | [WebPresenceEntry](#holomush-web-v1-WebPresenceEntry) | repeated |  |






<a name="holomush-web-v1-WebListPlayerSessionsRequest"></a>

### WebListPlayerSessionsRequest







<a name="holomush-web-v1-WebListPlayerSessionsResponse"></a>

### WebListPlayerSessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [WebPlayerSessionInfo](#holomush-web-v1-WebPlayerSessionInfo) | repeated |  |






<a name="holomush-web-v1-WebListSessionStreamsRequest"></a>

### WebListSessionStreamsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="holomush-web-v1-WebListSessionStreamsResponse"></a>

### WebListSessionStreamsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| streams | [string](#string) | repeated | event-store stream names (e.g. &#34;location:&lt;id&gt;&#34;, &#34;character:&lt;id&gt;&#34;) |






<a name="holomush-web-v1-WebLogoutRequest"></a>

### WebLogoutRequest







<a name="holomush-web-v1-WebLogoutResponse"></a>

### WebLogoutResponse







<a name="holomush-web-v1-WebPlayerSessionInfo"></a>

### WebPlayerSessionInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the PlayerSession.id (ULID). Used as target_session_id when revoking. Safe to show - resource handle, not a secret. |
| created_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |
| last_active | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |
| user_agent | [string](#string) |  |  |
| ip_address | [string](#string) |  |  |
| is_current | [bool](#bool) |  | is_current is true for the PlayerSession that made this request. |






<a name="holomush-web-v1-WebPresenceEntry"></a>

### WebPresenceEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| state | [WebPresenceState](#holomush-web-v1-WebPresenceState) |  |  |






<a name="holomush-web-v1-WebQueryStreamHistoryRequest"></a>

### WebQueryStreamHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| stream | [string](#string) |  |  |
| count | [int32](#int32) |  | page size; 0 = default (150), max 500, negative rejected |
| not_before_ms | [int64](#int64) |  | epoch ms time floor; 0 = no lower bound |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous WebQueryStreamHistoryResponse. Events older than the cursor position are returned. Empty = start from latest. |
| not_after_ms | [int64](#int64) |  | not_after_ms is the epoch-ms time ceiling. 0 = no upper bound (back-compat). INCLUSIVE: events with timestamp == not_after_ms are returned. Set by the web client to the Subscribe attach_moment_ms (carried on the REPLAY_COMPLETE ControlFrame) so backfill returns only events that existed before the live stream attached — eliminating the connect-time replay/backfill race (holomush-iu8j; holomush-fujt Fix B). |






<a name="holomush-web-v1-WebQueryStreamHistoryResponse"></a>

### WebQueryStreamHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [GameEvent](#holomush-web-v1-GameEvent) | repeated |  |
| has_more | [bool](#bool) |  |  |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next page. Empty if has_more is false. |






<a name="holomush-web-v1-WebRequestPasswordResetRequest"></a>

### WebRequestPasswordResetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |






<a name="holomush-web-v1-WebRequestPasswordResetResponse"></a>

### WebRequestPasswordResetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |






<a name="holomush-web-v1-WebRevokeOtherPlayerSessionsRequest"></a>

### WebRevokeOtherPlayerSessionsRequest







<a name="holomush-web-v1-WebRevokeOtherPlayerSessionsResponse"></a>

### WebRevokeOtherPlayerSessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| revoked_count | [int32](#int32) |  |  |






<a name="holomush-web-v1-WebRevokePlayerSessionRequest"></a>

### WebRevokePlayerSessionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| target_session_id | [string](#string) |  | target_session_id is the PlayerSession.id (ULID) to revoke. |






<a name="holomush-web-v1-WebRevokePlayerSessionResponse"></a>

### WebRevokePlayerSessionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |






<a name="holomush-web-v1-WebSelectCharacterRequest"></a>

### WebSelectCharacterRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |






<a name="holomush-web-v1-WebSelectCharacterResponse"></a>

### WebSelectCharacterResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| session_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| reattached | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |





 


<a name="holomush-web-v1-ControlSignal"></a>

### ControlSignal


| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 |  |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 |  |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 |  |
| CONTROL_SIGNAL_STREAM_OPENED | 3 | STREAM_OPENED is emitted as the first frame after a successful StreamEvents subscription. The accompanying ControlFrame.connection_id is the per-stream ULID — clients SHOULD store it and pass it back via SendCommandRequest.connection_id so the gateway routes per-connection commands (Phase 5 scene-focus autofocus) correctly under multi-tab. |



<a name="holomush-web-v1-EventChannel"></a>

### EventChannel


| Name | Number | Description |
| ---- | ------ | ----------- |
| EVENT_CHANNEL_UNSPECIFIED | 0 |  |
| EVENT_CHANNEL_TERMINAL | 1 |  |
| EVENT_CHANNEL_STATE | 2 |  |
| EVENT_CHANNEL_BOTH | 3 |  |
| EVENT_CHANNEL_AUDIT_ONLY | 4 | EVENT_CHANNEL_AUDIT_ONLY mirrors corev1.EventChannel for INV-GW-16 lockstep parity. These events are dropped at the gRPC Subscribe boundary and MUST NOT appear on the web wire format in practice. |



<a name="holomush-web-v1-WebPresenceContext"></a>

### WebPresenceContext


| Name | Number | Description |
| ---- | ------ | ----------- |
| WEB_PRESENCE_CONTEXT_UNSPECIFIED | 0 |  |
| WEB_PRESENCE_CONTEXT_LOCATION | 1 |  |
| WEB_PRESENCE_CONTEXT_SCENE | 2 |  |



<a name="holomush-web-v1-WebPresenceState"></a>

### WebPresenceState


| Name | Number | Description |
| ---- | ------ | ----------- |
| WEB_PRESENCE_STATE_UNSPECIFIED | 0 |  |
| WEB_PRESENCE_STATE_ACTIVE | 1 |  |
| WEB_PRESENCE_STATE_DETACHED | 2 |  |
| WEB_PRESENCE_STATE_INACTIVE | 3 |  |


 

 


<a name="holomush-web-v1-WebService"></a>

### WebService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SendCommand | [SendCommandRequest](#holomush-web-v1-SendCommandRequest) | [SendCommandResponse](#holomush-web-v1-SendCommandResponse) | Send a game command (say, pose, quit, etc.) |
| StreamEvents | [StreamEventsRequest](#holomush-web-v1-StreamEventsRequest) | [StreamEventsResponse](#holomush-web-v1-StreamEventsResponse) stream | Server-streaming event feed. Client receives game events (say, pose, arrive, leave) as they occur. |
| Disconnect | [DisconnectRequest](#holomush-web-v1-DisconnectRequest) | [DisconnectResponse](#holomush-web-v1-DisconnectResponse) | Disconnect ends the session and triggers cleanup. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-web-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-web-v1-GetCommandHistoryResponse) | Retrieve command history for a session. |
| WebAuthenticatePlayer | [WebAuthenticatePlayerRequest](#holomush-web-v1-WebAuthenticatePlayerRequest) | [WebAuthenticatePlayerResponse](#holomush-web-v1-WebAuthenticatePlayerResponse) | Web auth RPCs. |
| WebSelectCharacter | [WebSelectCharacterRequest](#holomush-web-v1-WebSelectCharacterRequest) | [WebSelectCharacterResponse](#holomush-web-v1-WebSelectCharacterResponse) |  |
| WebCreatePlayer | [WebCreatePlayerRequest](#holomush-web-v1-WebCreatePlayerRequest) | [WebCreatePlayerResponse](#holomush-web-v1-WebCreatePlayerResponse) |  |
| WebCreateGuest | [WebCreateGuestRequest](#holomush-web-v1-WebCreateGuestRequest) | [WebCreateGuestResponse](#holomush-web-v1-WebCreateGuestResponse) | Create an ephemeral guest player and character. |
| WebCreateCharacter | [WebCreateCharacterRequest](#holomush-web-v1-WebCreateCharacterRequest) | [WebCreateCharacterResponse](#holomush-web-v1-WebCreateCharacterResponse) |  |
| WebListCharacters | [WebListCharactersRequest](#holomush-web-v1-WebListCharactersRequest) | [WebListCharactersResponse](#holomush-web-v1-WebListCharactersResponse) |  |
| WebLogout | [WebLogoutRequest](#holomush-web-v1-WebLogoutRequest) | [WebLogoutResponse](#holomush-web-v1-WebLogoutResponse) |  |
| WebRequestPasswordReset | [WebRequestPasswordResetRequest](#holomush-web-v1-WebRequestPasswordResetRequest) | [WebRequestPasswordResetResponse](#holomush-web-v1-WebRequestPasswordResetResponse) |  |
| WebConfirmPasswordReset | [WebConfirmPasswordResetRequest](#holomush-web-v1-WebConfirmPasswordResetRequest) | [WebConfirmPasswordResetResponse](#holomush-web-v1-WebConfirmPasswordResetResponse) |  |
| WebCheckSession | [WebCheckSessionRequest](#holomush-web-v1-WebCheckSessionRequest) | [WebCheckSessionResponse](#holomush-web-v1-WebCheckSessionResponse) | Validate player session from cookie. Returns player info or Unauthenticated error. |
| WebGetContent | [WebGetContentRequest](#holomush-web-v1-WebGetContentRequest) | [WebGetContentResponse](#holomush-web-v1-WebGetContentResponse) | Content store access (public, no auth required). |
| WebListContent | [WebListContentRequest](#holomush-web-v1-WebListContentRequest) | [WebListContentResponse](#holomush-web-v1-WebListContentResponse) |  |
| WebQueryStreamHistory | [WebQueryStreamHistoryRequest](#holomush-web-v1-WebQueryStreamHistoryRequest) | [WebQueryStreamHistoryResponse](#holomush-web-v1-WebQueryStreamHistoryResponse) | WebQueryStreamHistory reads paginated event history for the web client. Proxies to CoreService.QueryStreamHistory — authorization is enforced by core. |
| WebListSessionStreams | [WebListSessionStreamsRequest](#holomush-web-v1-WebListSessionStreamsRequest) | [WebListSessionStreamsResponse](#holomush-web-v1-WebListSessionStreamsResponse) | WebListSessionStreams returns the stream names the session is subscribed to. Proxies to CoreService.ListSessionStreams — authorization is enforced by core. Used by the web client to enumerate streams for reload-backfill. |
| WebListPlayerSessions | [WebListPlayerSessionsRequest](#holomush-web-v1-WebListPlayerSessionsRequest) | [WebListPlayerSessionsResponse](#holomush-web-v1-WebListPlayerSessionsResponse) | Session-management RPCs. The caller is identified via the X-Session-Token cookie header injected by CookieMiddleware; no token field in the request. |
| WebRevokePlayerSession | [WebRevokePlayerSessionRequest](#holomush-web-v1-WebRevokePlayerSessionRequest) | [WebRevokePlayerSessionResponse](#holomush-web-v1-WebRevokePlayerSessionResponse) |  |
| WebRevokeOtherPlayerSessions | [WebRevokeOtherPlayerSessionsRequest](#holomush-web-v1-WebRevokeOtherPlayerSessionsRequest) | [WebRevokeOtherPlayerSessionsResponse](#holomush-web-v1-WebRevokeOtherPlayerSessionsResponse) |  |
| WebListFocusPresence | [WebListFocusPresenceRequest](#holomush-web-v1-WebListFocusPresenceRequest) | [WebListFocusPresenceResponse](#holomush-web-v1-WebListFocusPresenceResponse) | WebListFocusPresence returns the presence snapshot for the session&#39;s current focus context (location or scene). Proxies to CoreService.ListFocusPresence — authorization is enforced by core. player_session_token is read from the HTTP cookie by gateway middleware. |

 



<a name="holomush_control_v1_control-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/control/v1/control.proto



<a name="holomush-control-v1-ShutdownRequest"></a>

### ShutdownRequest
ShutdownRequest contains shutdown parameters.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| graceful | [bool](#bool) |  | If true, perform graceful shutdown allowing in-flight requests to complete. |






<a name="holomush-control-v1-ShutdownResponse"></a>

### ShutdownResponse
ShutdownResponse confirms shutdown initiation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | Human-readable status message. |






<a name="holomush-control-v1-StatusRequest"></a>

### StatusRequest
StatusRequest requests current process status.






<a name="holomush-control-v1-StatusResponse"></a>

### StatusResponse
StatusResponse contains current process status.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| running | [bool](#bool) |  | Whether the process is running. |
| pid | [int32](#int32) |  | Process ID. |
| uptime_seconds | [int64](#int64) |  | Seconds since process started. |
| component | [string](#string) |  | Component name (e.g., &#34;core&#34; or &#34;gateway&#34;). |





 

 

 


<a name="holomush-control-v1-ControlService"></a>

### ControlService
ControlService provides administrative operations for HoloMUSH processes.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Shutdown | [ShutdownRequest](#holomush-control-v1-ShutdownRequest) | [ShutdownResponse](#holomush-control-v1-ShutdownResponse) | Shutdown initiates process shutdown. |
| Status | [StatusRequest](#holomush-control-v1-StatusRequest) | [StatusResponse](#holomush-control-v1-StatusResponse) | Status returns current process status. |

 



<a name="holomush_plugin_v1_plugin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/plugin.proto
api/proto/holomush/plugin/v1/plugin.proto


<a name="holomush-plugin-v1-AuditDecisionHint"></a>

### AuditDecisionHint
AuditDecisionHint is a partial audit event emitted by a plugin handler.
The plugin provides decision-specific fields (id, name, message, effect,
resource, attributes, action qualifier); the host stamps identity fields
(subject from dispatch context, source = SourcePlugin, component =
plugin name, timestamp, duration).

Plugins MUST NOT set subject, source, or component — the dispatcher
overwrites those fields to prevent spoofing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Stable slug identifying the plugin&#39;s internal rule, e.g., &#34;not_member&#34;. |
| name | [string](#string) |  | Human-readable label for the rule, e.g., &#34;channels: not a member&#34;. |
| message | [string](#string) |  | Per-firing description, e.g., &#34;player not in channel members&#34;. |
| effect | [AuditEffect](#holomush-plugin-v1-AuditEffect) |  | Effect the plugin decided. Closed enum at the proto boundary — unknown effects can never round-trip the wire. |
| action_qualifier | [string](#string) |  | Action qualifier appended to the dispatcher-known base action. E.g., the dispatcher knows the command is &#34;channel&#34;; the plugin supplies &#34;speak&#34;, producing final action &#34;channel:speak&#34;. |
| resource | [string](#string) |  | Resource reference in &lt;type&gt;:&lt;id&gt; form, e.g., &#34;channel:01XYZ&#34;. Plugin-provided, host-validated for shape. |
| attributes | [AuditDecisionHint.AttributesEntry](#holomush-plugin-v1-AuditDecisionHint-AttributesEntry) | repeated | Plugin-provided context. Keys SHOULD be namespaced (e.g., &#34;channel.type&#34; rather than &#34;type&#34;) to avoid collision with host-overlay keys. |






<a name="holomush-plugin-v1-AuditDecisionHint-AttributesEntry"></a>

### AuditDecisionHint.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-plugin-v1-CommandRequest"></a>

### CommandRequest
CommandRequest carries context for a plugin command invocation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [string](#string) |  | Parsed command name (e.g., &#34;say&#34;, &#34;dig&#34;). |
| args | [string](#string) |  | Everything after the command name. |
| raw_input | [string](#string) |  | What the player actually typed (alias support). |
| character_id | [string](#string) |  | Invoking character ULID. |
| character_name | [string](#string) |  | Character display name. |
| location_id | [string](#string) |  | Character&#39;s current location ULID. |
| session_id | [string](#string) |  | Active session ULID. |
| player_id | [string](#string) |  | Player account ULID. |
| connection_id | [string](#string) |  | Originating connection ULID (Phase 5). Empty for server-side dispatch paths that do not have a specific connection (e.g., non-gateway callers). |






<a name="holomush-plugin-v1-CommandResponse"></a>

### CommandResponse
CommandResponse carries the result of a plugin command execution.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [CommandStatus](#holomush-plugin-v1-CommandStatus) |  | Outcome category. |
| output | [string](#string) |  | Synchronous text output to the invoking player. |
| events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events to append to the event store. |
| audit_hints | [AuditDecisionHint](#holomush-plugin-v1-AuditDecisionHint) | repeated | Audit decision hints accumulated by the plugin handler during this command dispatch. The dispatcher extracts these after the response is returned, stamps host-controlled fields (subject, action base, source, component, timestamp, duration), and flushes them through the audit logger. |






<a name="holomush-plugin-v1-EmitEvent"></a>

### EmitEvent
EmitEvent represents an event that a plugin wants to emit.
Compatible with pkg/plugin.EmitEvent.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  | Target stream for the event. |
| type | [string](#string) |  | Event type. |
| payload | [string](#string) |  | JSON-encoded payload. |






<a name="holomush-plugin-v1-Event"></a>

### Event
Event represents a game event delivered to plugins.
Compatible with pkg/plugin.Event but uses protobuf types.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Unique event identifier (ULID string). |
| stream | [string](#string) |  | Stream the event belongs to (e.g., &#34;location:loc_abc123&#34;). |
| type | [string](#string) |  | Event type (e.g., &#34;say&#34;, &#34;pose&#34;, &#34;arrive&#34;, &#34;leave&#34;, &#34;system&#34;). |
| timestamp | [int64](#int64) |  | Timestamp in Unix milliseconds. |
| actor_kind | [string](#string) |  | Actor kind as string (e.g., &#34;character&#34;, &#34;system&#34;, &#34;plugin&#34;). Using string instead of enum for flexibility and compatibility. |
| actor_id | [string](#string) |  | Actor identifier. |
| payload | [string](#string) |  | JSON-encoded payload. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination token for this event. Pass as PluginHostServiceQueryStreamHistoryRequest.cursor on the next call to page backward from this position. Empty on events received via delivery (not history). Treat as an opaque blob. |






<a name="holomush-plugin-v1-FocusFailure"></a>

### FocusFailure
FocusFailure carries the connection_id and reason for an AutoFocusOnJoin failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_id | [bytes](#bytes) |  |  |
| reason | [FocusFailureReason](#holomush-plugin-v1-FocusFailureReason) |  |  |






<a name="holomush-plugin-v1-FocusKey"></a>

### FocusKey
FocusKey identifies a focus membership within a session. A session&#39;s
focus memberships are unique by (kind, target_id) pair.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [FocusKind](#holomush-plugin-v1-FocusKind) |  |  |
| target_id | [string](#string) |  |  |






<a name="holomush-plugin-v1-HandleCommandRequest"></a>

### HandleCommandRequest
HandleCommandRequest wraps a command for delivery to the plugin.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [CommandRequest](#holomush-plugin-v1-CommandRequest) |  | The command to handle. |






<a name="holomush-plugin-v1-HandleCommandResponse"></a>

### HandleCommandResponse
HandleCommandResponse wraps the command result from the plugin.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| response | [CommandResponse](#holomush-plugin-v1-CommandResponse) |  | The command result. |






<a name="holomush-plugin-v1-HandleEventRequest"></a>

### HandleEventRequest
HandleEventRequest wraps an event for delivery to the plugin.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [Event](#holomush-plugin-v1-Event) |  | The event to handle. |






<a name="holomush-plugin-v1-HandleEventResponse"></a>

### HandleEventResponse
HandleEventResponse contains any events the plugin wants to emit.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| emit_events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events to emit in response. |






<a name="holomush-plugin-v1-InitRequest"></a>

### InitRequest
InitRequest is sent by the host after connecting to the plugin process.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [ServiceConfig](#holomush-plugin-v1-ServiceConfig) |  | Service configuration for the plugin. |






<a name="holomush-plugin-v1-InitResponse"></a>

### InitResponse
InitResponse is returned by the plugin after initialization.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provided_services | [string](#string) | repeated | gRPC service names this plugin provides on the go-plugin transport. |
| registered_emit_types | [string](#string) | repeated | Set of plugin-owned event types this plugin may emit. Host validates set-equality against manifest&#39;s crypto.emits per INV-S5. Plugins without crypto.emits leave empty and skip validation; plugins WITH crypto.emits MUST populate (mismatch fails load). |






<a name="holomush-plugin-v1-PluginHostServiceAddSessionStreamRequest"></a>

### PluginHostServiceAddSessionStreamRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Active session identifier. |
| stream | [string](#string) |  | Stream name to subscribe to (format: &#34;prefix:id&#34;). |
| replay_mode | [StreamReplayMode](#holomush-plugin-v1-StreamReplayMode) |  | replay_mode controls initial replay. Optional; defaults to FROM_CURSOR if unspecified for backwards compatibility. |






<a name="holomush-plugin-v1-PluginHostServiceAddSessionStreamResponse"></a>

### PluginHostServiceAddSessionStreamResponse







<a name="holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinRequest"></a>

### PluginHostServiceAutoFocusOnJoinRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [bytes](#bytes) |  | ULID |
| scene_id | [bytes](#bytes) |  | ULID |






<a name="holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinResponse"></a>

### PluginHostServiceAutoFocusOnJoinResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focused_connection_ids | [bytes](#bytes) | repeated |  |
| total_connection_count | [uint32](#uint32) |  |  |
| skipped_connection_ids | [bytes](#bytes) | repeated |  |
| failed_connection_ids | [FocusFailure](#holomush-plugin-v1-FocusFailure) | repeated |  |






<a name="holomush-plugin-v1-PluginHostServiceEmitEventRequest"></a>

### PluginHostServiceEmitEventRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  |  |
| event_type | [string](#string) |  |  |
| payload | [bytes](#bytes) |  |  |
| sensitive | [bool](#bool) |  | sensitive declares per-event sensitivity at emit time. Phase 3a&#39;s host-side fence at internal/plugin/event_emitter.go::Emit validates this against the plugin manifest&#39;s declared sensitivity: - manifest sensitivity=never: sensitive=true rejected (INV-6). - manifest sensitivity=may: sensitive=true|false honored. - manifest sensitivity=always: sensitive=false rejected (INV-7). Default false (proto3 zero) for older plugins compiled before this field existed — matching pre-Phase-3d behavior. |






<a name="holomush-plugin-v1-PluginHostServiceEmitEventResponse"></a>

### PluginHostServiceEmitEventResponse







<a name="holomush-plugin-v1-PluginHostServiceEvaluateRequest"></a>

### PluginHostServiceEvaluateRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action | [string](#string) |  |  |
| resource | [string](#string) |  | resource is a typed instance ref: &#34;scene:01ABC...&#34;. |






<a name="holomush-plugin-v1-PluginHostServiceEvaluateResponse"></a>

### PluginHostServiceEvaluateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| allowed | [bool](#bool) |  |  |
| reason | [string](#string) |  |  |
| matched_policy | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedRequest"></a>

### PluginHostServiceIsAnyConnFocusedRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [bytes](#bytes) |  | ULID |
| scene_id | [bytes](#bytes) |  | ULID |






<a name="holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedResponse"></a>

### PluginHostServiceIsAnyConnFocusedResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focused | [bool](#bool) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceJoinFocusRequest"></a>

### PluginHostServiceJoinFocusRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceJoinFocusResponse"></a>

### PluginHostServiceJoinFocusResponse







<a name="holomush-plugin-v1-PluginHostServiceKVDeleteRequest"></a>

### PluginHostServiceKVDeleteRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceKVDeleteResponse"></a>

### PluginHostServiceKVDeleteResponse







<a name="holomush-plugin-v1-PluginHostServiceKVGetRequest"></a>

### PluginHostServiceKVGetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceKVGetResponse"></a>

### PluginHostServiceKVGetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| found | [bool](#bool) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceKVSetRequest"></a>

### PluginHostServiceKVSetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceKVSetResponse"></a>

### PluginHostServiceKVSetResponse







<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetRequest"></a>

### PluginHostServiceLeaveFocusByTargetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetResponse"></a>

### PluginHostServiceLeaveFocusByTargetResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| succeeded | [int32](#int32) |  | Number of sessions successfully left. Zero is a valid result (target had no members or every member was already non-a-member — per-session idempotent no-ops count as successes). Callers comparing succeeded &#43; len(failed_session_ids) against total_scanned can distinguish total, partial, and empty-sweep outcomes without parsing any error string. |
| total_scanned | [int32](#int32) |  | Number of non-expired sessions the sweep scanned. Always &gt;= succeeded &#43; len(failed_session_ids). |
| failed_session_ids | [string](#string) | repeated | Session IDs for which the per-session leave failed. Empty means every scanned session succeeded (idempotent no-ops included). Per-session error details are not serialized; callers should treat these IDs as the authoritative partial-failure signal and re-issue LeaveFocus against them if retry is desired. The RPC error is reserved for enumeration/list failures (e.g., the session store could not list members). |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusRequest"></a>

### PluginHostServiceLeaveFocusRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusResponse"></a>

### PluginHostServiceLeaveFocusResponse







<a name="holomush-plugin-v1-PluginHostServiceLogRequest"></a>

### PluginHostServiceLogRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [string](#string) |  |  |
| message | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceLogResponse"></a>

### PluginHostServiceLogResponse







<a name="holomush-plugin-v1-PluginHostServicePresentFocusRequest"></a>

### PluginHostServicePresentFocusRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  |  |






<a name="holomush-plugin-v1-PluginHostServicePresentFocusResponse"></a>

### PluginHostServicePresentFocusResponse







<a name="holomush-plugin-v1-PluginHostServiceQueryStreamHistoryRequest"></a>

### PluginHostServiceQueryStreamHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  |  |
| count | [int32](#int32) |  |  |
| not_before_ms | [int64](#int64) |  | Epoch milliseconds. Events before this time are excluded. 0 means no lower bound. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous response. Events older than the cursor position are returned. Empty = start from latest. |






<a name="holomush-plugin-v1-PluginHostServiceQueryStreamHistoryResponse"></a>

### PluginHostServiceQueryStreamHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [Event](#holomush-plugin-v1-Event) | repeated |  |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next page. Empty if no more pages. |






<a name="holomush-plugin-v1-PluginHostServiceRemoveSessionStreamRequest"></a>

### PluginHostServiceRemoveSessionStreamRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| stream | [string](#string) |  |  |






<a name="holomush-plugin-v1-PluginHostServiceRemoveSessionStreamResponse"></a>

### PluginHostServiceRemoveSessionStreamResponse







<a name="holomush-plugin-v1-PluginHostServiceRequestEmitTokenRequest"></a>

### PluginHostServiceRequestEmitTokenRequest
PluginHostServiceRequestEmitTokenRequest carries no fields. The host
derives the calling plugin&#39;s identity from the mTLS-bound server struct.
Future evolution: do NOT add actor fields here — that would re-open the
G1 forgery surface this RPC is designed to close.






<a name="holomush-plugin-v1-PluginHostServiceRequestEmitTokenResponse"></a>

### PluginHostServiceRequestEmitTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | Opaque self-token. Plugins MUST treat this as opaque; only the host&#39;s emitTokenStore can interpret it. The token is bound to ActorPlugin &#43; the calling plugin&#39;s name and is single-use-friendly (TTL-revoked). |






<a name="holomush-plugin-v1-PluginHostServiceSetConnectionFocusRequest"></a>

### PluginHostServiceSetConnectionFocusRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_id | [bytes](#bytes) |  | ULID |
| focus_key | [FocusKey](#holomush-plugin-v1-FocusKey) | optional |  |
| is_scene_grid | [bool](#bool) |  | is_scene_grid signals that this call originated from a `scene grid` command — substrate skips the D9 PresentingFocus write per D10. |






<a name="holomush-plugin-v1-PluginHostServiceSetConnectionFocusResponse"></a>

### PluginHostServiceSetConnectionFocusResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focus_key | [FocusKey](#holomush-plugin-v1-FocusKey) | optional |  |






<a name="holomush-plugin-v1-QuerySessionStreamsRequest"></a>

### QuerySessionStreamsRequest
QuerySessionStreamsRequest provides session context for stream contribution.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Identifier of the character entering the session. |
| player_id | [string](#string) |  | Identifier of the player owning the character. |
| session_id | [string](#string) |  | Session identifier. |






<a name="holomush-plugin-v1-QuerySessionStreamsResponse"></a>

### QuerySessionStreamsResponse
QuerySessionStreamsResponse returns stream names and an optional error.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| streams | [string](#string) | repeated | Stream names the plugin wants added to this session&#39;s subscription. |
| error | [string](#string) |  | Non-empty indicates a plugin-reported error. Host degrades (logs &#43; skips). |






<a name="holomush-plugin-v1-ServiceConfig"></a>

### ServiceConfig
ServiceConfig carries initialization data from the host to the plugin.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_string | [string](#string) |  | PostgreSQL connection string (provided when the plugin declares storage: postgres). |
| required_services | [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry) | repeated | Addresses of required services, keyed by service name (future use). |
| plugin_config | [ServiceConfig.PluginConfigEntry](#holomush-plugin-v1-ServiceConfig-PluginConfigEntry) | repeated | Opaque plugin-owned runtime config: the effective (manifest-default &lt; server-override) map the host delivers at init. The host does NOT interpret keys/values; the plugin decodes them per its own schema. |






<a name="holomush-plugin-v1-ServiceConfig-PluginConfigEntry"></a>

### ServiceConfig.PluginConfigEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-plugin-v1-ServiceConfig-RequiredServicesEntry"></a>

### ServiceConfig.RequiredServicesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |





 


<a name="holomush-plugin-v1-AuditEffect"></a>

### AuditEffect
AuditEffect is the closed set of decision outcomes a plugin handler may
emit through an AuditDecisionHint. Plugin denials and plugin allows are
the only meaningful outcomes — engine-specific effects (default_deny,
system_bypass) are not exposed to plugins because plugins do not produce
those decisions.

| Name | Number | Description |
| ---- | ------ | ----------- |
| AUDIT_EFFECT_UNSPECIFIED | 0 |  |
| AUDIT_EFFECT_DENY | 1 |  |
| AUDIT_EFFECT_ALLOW | 2 |  |



<a name="holomush-plugin-v1-CommandStatus"></a>

### CommandStatus
CommandStatus maps to pkg/plugin.CommandStatus values.

| Name | Number | Description |
| ---- | ------ | ----------- |
| COMMAND_STATUS_UNSPECIFIED | 0 |  |
| COMMAND_STATUS_OK | 1 |  |
| COMMAND_STATUS_ERROR | 2 |  |
| COMMAND_STATUS_FAILURE | 3 |  |
| COMMAND_STATUS_FATAL | 4 |  |



<a name="holomush-plugin-v1-FocusFailureReason"></a>

### FocusFailureReason


| Name | Number | Description |
| ---- | ------ | ----------- |
| FOCUS_FAILURE_REASON_UNSPECIFIED | 0 |  |
| FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT | 1 |  |
| FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND | 2 |  |



<a name="holomush-plugin-v1-FocusKind"></a>

### FocusKind
FocusKind enumerates the types of focused contexts a character can
participate in. Adding a new kind requires: (a) a new constant here,
(b) a matching session.FocusKind constant in Go, (c) a new
FocusKindPolicy implementation registered in the coordinator.

| Name | Number | Description |
| ---- | ------ | ----------- |
| FOCUS_KIND_UNSPECIFIED | 0 |  |
| FOCUS_KIND_SCENE | 1 |  |



<a name="holomush-plugin-v1-StreamReplayMode"></a>

### StreamReplayMode
StreamReplayMode controls how a stream subscription&#39;s initial replay
behaves when added via AddSessionStream.

| Name | Number | Description |
| ---- | ------ | ----------- |
| STREAM_REPLAY_MODE_UNSPECIFIED | 0 |  |
| STREAM_REPLAY_MODE_FROM_CURSOR | 1 |  |
| STREAM_REPLAY_MODE_LIVE_ONLY | 2 |  |


 

 


<a name="holomush-plugin-v1-PluginHostService"></a>

### PluginHostService
PluginHostService runs in the host process, allowing binary plugins
to call back for event emission, logging, and KV storage.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| EmitEvent | [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest) | [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse) | EmitEvent publishes an event to a stream. |
| Log | [PluginHostServiceLogRequest](#holomush-plugin-v1-PluginHostServiceLogRequest) | [PluginHostServiceLogResponse](#holomush-plugin-v1-PluginHostServiceLogResponse) | Log writes a log message through the host&#39;s logging system. |
| KVGet | [PluginHostServiceKVGetRequest](#holomush-plugin-v1-PluginHostServiceKVGetRequest) | [PluginHostServiceKVGetResponse](#holomush-plugin-v1-PluginHostServiceKVGetResponse) | KVGet retrieves a value from the plugin&#39;s key-value store. |
| KVSet | [PluginHostServiceKVSetRequest](#holomush-plugin-v1-PluginHostServiceKVSetRequest) | [PluginHostServiceKVSetResponse](#holomush-plugin-v1-PluginHostServiceKVSetResponse) | KVSet stores a value in the plugin&#39;s key-value store. |
| KVDelete | [PluginHostServiceKVDeleteRequest](#holomush-plugin-v1-PluginHostServiceKVDeleteRequest) | [PluginHostServiceKVDeleteResponse](#holomush-plugin-v1-PluginHostServiceKVDeleteResponse) | KVDelete removes a value from the plugin&#39;s key-value store. |
| AddSessionStream | [PluginHostServiceAddSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceAddSessionStreamRequest) | [PluginHostServiceAddSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceAddSessionStreamResponse) | AddSessionStream subscribes an active session to an additional stream mid-session. Returns SESSION_NOT_FOUND (codes.NotFound) if session_id is not active. |
| RemoveSessionStream | [PluginHostServiceRemoveSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamRequest) | [PluginHostServiceRemoveSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamResponse) | RemoveSessionStream unsubscribes an active session from a stream. Idempotent: returns success if stream is not subscribed. |
| JoinFocus | [PluginHostServiceJoinFocusRequest](#holomush-plugin-v1-PluginHostServiceJoinFocusRequest) | [PluginHostServiceJoinFocusResponse](#holomush-plugin-v1-PluginHostServiceJoinFocusResponse) | JoinFocus adds a focus membership to an active or detached session. Plugins declare intent; the server applies kind-specific replay policy. |
| LeaveFocus | [PluginHostServiceLeaveFocusRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusRequest) | [PluginHostServiceLeaveFocusResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusResponse) | LeaveFocus removes a focus membership. Idempotent on non-member. |
| LeaveFocusByTarget | [PluginHostServiceLeaveFocusByTargetRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetRequest) | [PluginHostServiceLeaveFocusByTargetResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetResponse) | LeaveFocusByTarget removes the given focus membership from every non-expired session that holds it. Used for cross-session fan-out (e.g., scene-end reaches all participants). Partial success is normal: individual session failures are aggregated without halting the sweep. |
| PresentFocus | [PluginHostServicePresentFocusRequest](#holomush-plugin-v1-PluginHostServicePresentFocusRequest) | [PluginHostServicePresentFocusResponse](#holomush-plugin-v1-PluginHostServicePresentFocusResponse) | PresentFocus updates the session&#39;s PresentingFocus pointer. Target MUST already exist in FocusMemberships. |
| QueryStreamHistory | [PluginHostServiceQueryStreamHistoryRequest](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryRequest) | [PluginHostServiceQueryStreamHistoryResponse](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryResponse) | QueryStreamHistory reads the tail of a stream for plugin-side display. Read-only: does not advance cursors or affect session state. Count capped at 500 server-side. |
| DecryptOwnAuditRows | [DecryptOwnAuditRowsRequest](#holomush-plugin-v1-DecryptOwnAuditRowsRequest) | [DecryptOwnAuditRowsResponse](#holomush-plugin-v1-DecryptOwnAuditRowsResponse) | DecryptOwnAuditRows decrypts a batch of the calling plugin&#39;s OWN audit rows host-side. The plugin never holds a DEK. Per-row result envelope (INV-RB-12). Batch capped at 500 server-side (REJECT, not clamp). Authorization: OwnerMap subject ownership (g1) &#43; crypto.emits[].readback manifest flag (g2) (INV-RB-2). Request / response message shapes live in audit.proto (AuditRow domain). |
| RequestEmitToken | [PluginHostServiceRequestEmitTokenRequest](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenRequest) | [PluginHostServiceRequestEmitTokenResponse](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenResponse) | RequestEmitToken issues a self-token bound to the calling plugin&#39;s identity (ActorPlugin &#43; pluginName), so plugin-served gRPC handlers (which are not invoked via DeliverEvent / DeliverCommand) can still call EmitEvent. The plugin&#39;s identity is taken from the mTLS-bound gRPC server struct — the request carries no identity fields and the plugin cannot impersonate another actor through this RPC. (Spec §3.3.5 / §5.4 self-token pattern.) |
| SetConnectionFocus | [PluginHostServiceSetConnectionFocusRequest](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusRequest) | [PluginHostServiceSetConnectionFocusResponse](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusResponse) | SetConnectionFocus — Phase 5 explicit focus mutation for one Connection. Substrate validates membership against FocusMemberships (D4); writes Connection.FocusKey &#43; (D9-gated) Info.PresentingFocus atomically under one Store-lock acquisition (D7). |
| AutoFocusOnJoin | [PluginHostServiceAutoFocusOnJoinRequest](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinRequest) | [PluginHostServiceAutoFocusOnJoinResponse](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinResponse) | AutoFocusOnJoin — Phase 5 fan-out: focuses all terminal/telnet connections of the character on the given scene. Skips conns already explicitly focused elsewhere (D8). Caller must have completed JoinFocus before invocation. |
| IsAnyConnFocused | [PluginHostServiceIsAnyConnFocusedRequest](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedRequest) | [PluginHostServiceIsAnyConnFocusedResponse](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedResponse) | IsAnyConnFocused — Phase 5 notification-emission helper: true iff any of the character&#39;s connections has FocusKey == {scene, scene_id}. |
| Evaluate | [PluginHostServiceEvaluateRequest](#holomush-plugin-v1-PluginHostServiceEvaluateRequest) | [PluginHostServiceEvaluateResponse](#holomush-plugin-v1-PluginHostServiceEvaluateResponse) | Evaluate runs the host ABAC engine for a single action against a single resource instance owned by the calling plugin. The subject is derived host-side from the dispatch token (see EmitEvent) — there is no subject field on the wire (spec §2, INV-1). |


<a name="holomush-plugin-v1-PluginService"></a>

### PluginService
PluginService is called by the go-plugin host to send events and commands to binary plugins.
This service is implemented by the plugin (the gRPC server runs in the plugin process).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Init | [InitRequest](#holomush-plugin-v1-InitRequest) | [InitResponse](#holomush-plugin-v1-InitResponse) | Init is called by the host after connection, providing service configuration (DB connection string, required service addresses, etc.) and receiving the list of gRPC services the plugin provides. |
| HandleEvent | [HandleEventRequest](#holomush-plugin-v1-HandleEventRequest) | [HandleEventResponse](#holomush-plugin-v1-HandleEventResponse) | HandleEvent delivers an event to the plugin and receives any response events. |
| HandleCommand | [HandleCommandRequest](#holomush-plugin-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-plugin-v1-HandleCommandResponse) | HandleCommand delivers a command to the plugin. |
| QuerySessionStreams | [QuerySessionStreamsRequest](#holomush-plugin-v1-QuerySessionStreamsRequest) | [QuerySessionStreamsResponse](#holomush-plugin-v1-QuerySessionStreamsResponse) | QuerySessionStreams returns stream names the plugin wants subscribed for a session. Called once at session establishment, before LISTEN setup. Only invoked for plugins that declare session_streams: true in their manifest. |

 



<a name="holomush_plugin_v1_hostfunc-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/hostfunc.proto
api/proto/holomush/plugin/v1/hostfunc.proto


<a name="holomush-plugin-v1-AddSessionStreamRequest"></a>

### AddSessionStreamRequest
AddSessionStreamRequest specifies which session and stream to add.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Active session identifier. |
| stream | [string](#string) |  | Stream name to subscribe to (format: &#34;prefix:id&#34;). |






<a name="holomush-plugin-v1-AddSessionStreamResponse"></a>

### AddSessionStreamResponse
AddSessionStreamResponse indicates success or failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the stream was successfully added. |
| error | [string](#string) |  | Non-empty on error. |






<a name="holomush-plugin-v1-CharacterInfo"></a>

### CharacterInfo
CharacterInfo contains basic character information.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Character identifier. |
| name | [string](#string) |  | Character name. |






<a name="holomush-plugin-v1-CommandHelpInfo"></a>

### CommandHelpInfo
CommandHelpInfo contains detailed help for a command.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Command name. |
| help | [string](#string) |  | Short help description. |
| usage | [string](#string) |  | Usage pattern. |
| help_text | [string](#string) |  | Detailed markdown help text. |
| capabilities | [string](#string) | repeated | Required capabilities for this command. |
| source | [string](#string) |  | Source plugin name or &#34;core&#34;. |






<a name="holomush-plugin-v1-CommandInfo"></a>

### CommandInfo
CommandInfo contains basic command information for listing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Command name. |
| help | [string](#string) |  | Short help description. |
| usage | [string](#string) |  | Usage pattern (e.g., &#34;say &lt;message&gt;&#34;). |
| source | [string](#string) |  | Source plugin name or &#34;core&#34;. |






<a name="holomush-plugin-v1-EmitEventRequest"></a>

### EmitEventRequest
EmitEventRequest wraps an event for emission by the host.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [EmitEvent](#holomush-plugin-v1-EmitEvent) |  | The event to emit. |






<a name="holomush-plugin-v1-EmitEventResponse"></a>

### EmitEventResponse
EmitEventResponse indicates success or failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the event was successfully emitted. |
| error | [string](#string) |  | Error message if success is false. |






<a name="holomush-plugin-v1-GetCommandHelpRequest"></a>

### GetCommandHelpRequest
GetCommandHelpRequest requests detailed help for a command.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_name | [string](#string) |  | Command name to get help for. |






<a name="holomush-plugin-v1-GetCommandHelpResponse"></a>

### GetCommandHelpResponse
GetCommandHelpResponse contains detailed command help.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [CommandHelpInfo](#holomush-plugin-v1-CommandHelpInfo) |  | Detailed command information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-KVDeleteRequest"></a>

### KVDeleteRequest
KVDeleteRequest removes a key.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to delete. |






<a name="holomush-plugin-v1-KVDeleteResponse"></a>

### KVDeleteResponse
KVDeleteResponse indicates success or failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| deleted | [bool](#bool) |  | Whether the key was deleted (true even if key didn&#39;t exist). |
| error | [string](#string) |  | Error message if deletion failed. |






<a name="holomush-plugin-v1-KVGetRequest"></a>

### KVGetRequest
KVGetRequest retrieves a value by key.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to look up. |






<a name="holomush-plugin-v1-KVGetResponse"></a>

### KVGetResponse
KVGetResponse contains the retrieved value.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [bytes](#bytes) |  | Value if found. |
| found | [bool](#bool) |  | Whether the key was found. |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-KVSetRequest"></a>

### KVSetRequest
KVSetRequest stores a key-value pair.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to store. |
| value | [bytes](#bytes) |  | Value to store. |






<a name="holomush-plugin-v1-KVSetResponse"></a>

### KVSetResponse
KVSetResponse indicates success or failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the value was successfully stored. |
| error | [string](#string) |  | Error message if success is false. |






<a name="holomush-plugin-v1-ListCommandsRequest"></a>

### ListCommandsRequest
ListCommandsRequest requests the list of available commands.
Commands are filtered by the character&#39;s capabilities.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Character identifier for capability filtering. |






<a name="holomush-plugin-v1-ListCommandsResponse"></a>

### ListCommandsResponse
ListCommandsResponse contains the list of available commands.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [CommandInfo](#holomush-plugin-v1-CommandInfo) | repeated | Available commands. |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-LocationInfo"></a>

### LocationInfo
LocationInfo contains basic location information.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Location identifier. |
| name | [string](#string) |  | Location name/title. |
| description | [string](#string) |  | Location description. |






<a name="holomush-plugin-v1-LogRequest"></a>

### LogRequest
LogRequest writes a log message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [LogLevel](#holomush-plugin-v1-LogLevel) |  | Log level. |
| message | [string](#string) |  | Log message. |
| fields | [LogRequest.FieldsEntry](#holomush-plugin-v1-LogRequest-FieldsEntry) | repeated | Additional structured fields. |






<a name="holomush-plugin-v1-LogRequest-FieldsEntry"></a>

### LogRequest.FieldsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-plugin-v1-LogResponse"></a>

### LogResponse
LogResponse is empty (logging is fire-and-forget).






<a name="holomush-plugin-v1-QueryCharacterRequest"></a>

### QueryCharacterRequest
QueryCharacterRequest requests information about a character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Character identifier. |






<a name="holomush-plugin-v1-QueryCharacterResponse"></a>

### QueryCharacterResponse
QueryCharacterResponse contains character information.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character | [CharacterInfo](#holomush-plugin-v1-CharacterInfo) |  | Character information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-QueryLocationCharactersRequest"></a>

### QueryLocationCharactersRequest
QueryLocationCharactersRequest requests all characters in a location.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location_id | [string](#string) |  | Location identifier. |






<a name="holomush-plugin-v1-QueryLocationCharactersResponse"></a>

### QueryLocationCharactersResponse
QueryLocationCharactersResponse contains the list of characters.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterInfo](#holomush-plugin-v1-CharacterInfo) | repeated | Characters in the location. |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-QueryLocationRequest"></a>

### QueryLocationRequest
QueryLocationRequest requests information about a location.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location_id | [string](#string) |  | Location identifier. |






<a name="holomush-plugin-v1-QueryLocationResponse"></a>

### QueryLocationResponse
QueryLocationResponse contains location information.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location | [LocationInfo](#holomush-plugin-v1-LocationInfo) |  | Location information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |






<a name="holomush-plugin-v1-RemoveSessionStreamRequest"></a>

### RemoveSessionStreamRequest
RemoveSessionStreamRequest specifies which stream to remove.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Active session identifier. |
| stream | [string](#string) |  | Stream name to unsubscribe from. |






<a name="holomush-plugin-v1-RemoveSessionStreamResponse"></a>

### RemoveSessionStreamResponse
RemoveSessionStreamResponse indicates success or failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the stream was successfully removed (true even if not subscribed). |
| error | [string](#string) |  | Non-empty on error. |





 


<a name="holomush-plugin-v1-LogLevel"></a>

### LogLevel
LogLevel specifies the severity of a log message.

| Name | Number | Description |
| ---- | ------ | ----------- |
| LOG_LEVEL_UNSPECIFIED | 0 |  |
| LOG_LEVEL_DEBUG | 1 |  |
| LOG_LEVEL_INFO | 2 |  |
| LOG_LEVEL_WARN | 3 |  |
| LOG_LEVEL_ERROR | 4 |  |


 

 


<a name="holomush-plugin-v1-HostFunctionsService"></a>

### HostFunctionsService
HostFunctionsService provides host capabilities to plugins.
This service is implemented by the host (the gRPC server runs in the host process).
Plugins call these methods to interact with the game world.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| EmitEvent | [EmitEventRequest](#holomush-plugin-v1-EmitEventRequest) | [EmitEventResponse](#holomush-plugin-v1-EmitEventResponse) | EmitEvent publishes an event to a stream. |
| QueryLocation | [QueryLocationRequest](#holomush-plugin-v1-QueryLocationRequest) | [QueryLocationResponse](#holomush-plugin-v1-QueryLocationResponse) | QueryLocation retrieves information about a location. |
| QueryCharacter | [QueryCharacterRequest](#holomush-plugin-v1-QueryCharacterRequest) | [QueryCharacterResponse](#holomush-plugin-v1-QueryCharacterResponse) | QueryCharacter retrieves information about a character. |
| QueryLocationCharacters | [QueryLocationCharactersRequest](#holomush-plugin-v1-QueryLocationCharactersRequest) | [QueryLocationCharactersResponse](#holomush-plugin-v1-QueryLocationCharactersResponse) | QueryLocationCharacters retrieves all characters in a location. |
| KVGet | [KVGetRequest](#holomush-plugin-v1-KVGetRequest) | [KVGetResponse](#holomush-plugin-v1-KVGetResponse) | KVGet retrieves a value from the plugin&#39;s key-value store. |
| KVSet | [KVSetRequest](#holomush-plugin-v1-KVSetRequest) | [KVSetResponse](#holomush-plugin-v1-KVSetResponse) | KVSet stores a value in the plugin&#39;s key-value store. |
| KVDelete | [KVDeleteRequest](#holomush-plugin-v1-KVDeleteRequest) | [KVDeleteResponse](#holomush-plugin-v1-KVDeleteResponse) | KVDelete removes a value from the plugin&#39;s key-value store. |
| Log | [LogRequest](#holomush-plugin-v1-LogRequest) | [LogResponse](#holomush-plugin-v1-LogResponse) | Log writes a log message through the host&#39;s logging system. |
| ListCommands | [ListCommandsRequest](#holomush-plugin-v1-ListCommandsRequest) | [ListCommandsResponse](#holomush-plugin-v1-ListCommandsResponse) | ListCommands returns all available commands. Requires capability: command.list |
| GetCommandHelp | [GetCommandHelpRequest](#holomush-plugin-v1-GetCommandHelpRequest) | [GetCommandHelpResponse](#holomush-plugin-v1-GetCommandHelpResponse) | GetCommandHelp returns detailed help for a specific command. Requires capability: command.help |
| AddSessionStream | [AddSessionStreamRequest](#holomush-plugin-v1-AddSessionStreamRequest) | [AddSessionStreamResponse](#holomush-plugin-v1-AddSessionStreamResponse) | AddSessionStream subscribes an active session to an additional stream mid-session. Returns SESSION_NOT_FOUND (codes.NotFound) if session_id is not active. |
| RemoveSessionStream | [RemoveSessionStreamRequest](#holomush-plugin-v1-RemoveSessionStreamRequest) | [RemoveSessionStreamResponse](#holomush-plugin-v1-RemoveSessionStreamResponse) | RemoveSessionStream unsubscribes an active session from a stream. Idempotent: returns success if stream is not subscribed. |

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

