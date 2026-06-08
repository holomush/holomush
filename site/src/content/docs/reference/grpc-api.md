---
title: "gRPC API Reference"
---

<a name="top"></a>

## Table of Contents

- [holomush/core/v1/core.proto](#holomush_core_v1_core-proto)
    - [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest)
    - [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse)
    - [AvailableCommand](#holomush-core-v1-AvailableCommand)
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
    - [ListAvailableCommandsRequest](#holomush-core-v1-ListAvailableCommandsRequest)
    - [ListAvailableCommandsResponse](#holomush-core-v1-ListAvailableCommandsResponse)
    - [ListAvailableCommandsResponse.AliasesEntry](#holomush-core-v1-ListAvailableCommandsResponse-AliasesEntry)
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
    - [RefreshConnectionRequest](#holomush-core-v1-RefreshConnectionRequest)
    - [RefreshConnectionResponse](#holomush-core-v1-RefreshConnectionResponse)
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
  
- [holomush/admin/v1/read_stream.proto](#holomush_admin_v1_read_stream-proto)
    - [AdminReadStreamRequest](#holomush-admin-v1-AdminReadStreamRequest)
    - [AdminReadStreamResponse](#holomush-admin-v1-AdminReadStreamResponse)
    - [ContextRef](#holomush-admin-v1-ContextRef)
    - [PendingApproval](#holomush-admin-v1-PendingApproval)
    - [ReadFinished](#holomush-admin-v1-ReadFinished)
    - [ReadStarted](#holomush-admin-v1-ReadStarted)
  
    - [ReadFinished.TerminatedBy](#holomush-admin-v1-ReadFinished-TerminatedBy)
  
- [holomush/admin/v1/rekey.proto](#holomush_admin_v1_rekey-proto)
    - [Phase3Progress](#holomush-admin-v1-Phase3Progress)
    - [Phase5Attempt](#holomush-admin-v1-Phase5Attempt)
    - [PhaseCompleted](#holomush-admin-v1-PhaseCompleted)
    - [PhaseStarted](#holomush-admin-v1-PhaseStarted)
    - [RekeyAbortRequest](#holomush-admin-v1-RekeyAbortRequest)
    - [RekeyAbortResponse](#holomush-admin-v1-RekeyAbortResponse)
    - [RekeyCompleted](#holomush-admin-v1-RekeyCompleted)
    - [RekeyError](#holomush-admin-v1-RekeyError)
    - [RekeyListRequest](#holomush-admin-v1-RekeyListRequest)
    - [RekeyProgress](#holomush-admin-v1-RekeyProgress)
    - [RekeyRequest](#holomush-admin-v1-RekeyRequest)
    - [RekeyResumeRequest](#holomush-admin-v1-RekeyResumeRequest)
    - [RekeyStatusRequest](#holomush-admin-v1-RekeyStatusRequest)
    - [RekeyStatusResponse](#holomush-admin-v1-RekeyStatusResponse)
  
- [holomush/admin/v1/admin.proto](#holomush_admin_v1_admin-proto)
    - [ApproveRequest](#holomush-admin-v1-ApproveRequest)
    - [ApproveResponse](#holomush-admin-v1-ApproveResponse)
    - [AuthenticateRequest](#holomush-admin-v1-AuthenticateRequest)
    - [AuthenticateResponse](#holomush-admin-v1-AuthenticateResponse)
    - [ResetTOTPRequest](#holomush-admin-v1-ResetTOTPRequest)
    - [ResetTOTPResponse](#holomush-admin-v1-ResetTOTPResponse)
    - [StatusRequest](#holomush-admin-v1-StatusRequest)
    - [StatusResponse](#holomush-admin-v1-StatusResponse)
  
    - [AdminService](#holomush-admin-v1-AdminService)
  
- [holomush/content/v1/content.proto](#holomush_content_v1_content-proto)
    - [ContentItem](#holomush-content-v1-ContentItem)
    - [ContentItem.MetadataEntry](#holomush-content-v1-ContentItem-MetadataEntry)
    - [GetContentRequest](#holomush-content-v1-GetContentRequest)
    - [GetContentResponse](#holomush-content-v1-GetContentResponse)
    - [ListContentRequest](#holomush-content-v1-ListContentRequest)
    - [ListContentResponse](#holomush-content-v1-ListContentResponse)
  
    - [ContentService](#holomush-content-v1-ContentService)
  
- [holomush/control/v1/control.proto](#holomush_control_v1_control-proto)
    - [ShutdownRequest](#holomush-control-v1-ShutdownRequest)
    - [ShutdownResponse](#holomush-control-v1-ShutdownResponse)
    - [StatusRequest](#holomush-control-v1-StatusRequest)
    - [StatusResponse](#holomush-control-v1-StatusResponse)
  
    - [ControlService](#holomush-control-v1-ControlService)
  
- [holomush/eventbus/v1/eventbus.proto](#holomush_eventbus_v1_eventbus-proto)
    - [Actor](#holomush-eventbus-v1-Actor)
    - [Event](#holomush-eventbus-v1-Event)
  
    - [ActorKind](#holomush-eventbus-v1-ActorKind)
  
- [holomush/plugin/v1/attribute.proto](#holomush_plugin_v1_attribute-proto)
    - [AttributeValue](#holomush-plugin-v1-AttributeValue)
    - [GetSchemaRequest](#holomush-plugin-v1-GetSchemaRequest)
    - [GetSchemaResponse](#holomush-plugin-v1-GetSchemaResponse)
    - [GetSchemaResponse.ResourceTypesEntry](#holomush-plugin-v1-GetSchemaResponse-ResourceTypesEntry)
    - [ResolveResourceRequest](#holomush-plugin-v1-ResolveResourceRequest)
    - [ResolveResourceResponse](#holomush-plugin-v1-ResolveResourceResponse)
    - [ResolveResourceResponse.AttributesEntry](#holomush-plugin-v1-ResolveResourceResponse-AttributesEntry)
    - [ResourceTypeSchema](#holomush-plugin-v1-ResourceTypeSchema)
    - [ResourceTypeSchema.AttributesEntry](#holomush-plugin-v1-ResourceTypeSchema-AttributesEntry)
    - [StringList](#holomush-plugin-v1-StringList)
  
    - [AttributeType](#holomush-plugin-v1-AttributeType)
  
    - [AttributeResolverService](#holomush-plugin-v1-AttributeResolverService)
  
- [holomush/plugin/v1/audit.proto](#holomush_plugin_v1_audit-proto)
    - [AuditEventRequest](#holomush-plugin-v1-AuditEventRequest)
    - [AuditEventResponse](#holomush-plugin-v1-AuditEventResponse)
    - [AuditRow](#holomush-plugin-v1-AuditRow)
    - [DecryptOwnAuditRowsRequest](#holomush-plugin-v1-DecryptOwnAuditRowsRequest)
    - [DecryptOwnAuditRowsResponse](#holomush-plugin-v1-DecryptOwnAuditRowsResponse)
    - [QueryHistoryRequest](#holomush-plugin-v1-QueryHistoryRequest)
    - [QueryHistoryResponse](#holomush-plugin-v1-QueryHistoryResponse)
    - [RowResult](#holomush-plugin-v1-RowResult)
  
    - [PluginAuditService](#holomush-plugin-v1-PluginAuditService)
  
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
    - [PluginHostServiceCommandInfo](#holomush-plugin-v1-PluginHostServiceCommandInfo)
    - [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest)
    - [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse)
    - [PluginHostServiceEvaluateRequest](#holomush-plugin-v1-PluginHostServiceEvaluateRequest)
    - [PluginHostServiceEvaluateResponse](#holomush-plugin-v1-PluginHostServiceEvaluateResponse)
    - [PluginHostServiceGetCommandHelpRequest](#holomush-plugin-v1-PluginHostServiceGetCommandHelpRequest)
    - [PluginHostServiceGetCommandHelpResponse](#holomush-plugin-v1-PluginHostServiceGetCommandHelpResponse)
    - [PluginHostServiceGetConnectionFocusRequest](#holomush-plugin-v1-PluginHostServiceGetConnectionFocusRequest)
    - [PluginHostServiceGetConnectionFocusResponse](#holomush-plugin-v1-PluginHostServiceGetConnectionFocusResponse)
    - [PluginHostServiceGetSettingRequest](#holomush-plugin-v1-PluginHostServiceGetSettingRequest)
    - [PluginHostServiceGetSettingResponse](#holomush-plugin-v1-PluginHostServiceGetSettingResponse)
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
    - [PluginHostServiceListCommandsRequest](#holomush-plugin-v1-PluginHostServiceListCommandsRequest)
    - [PluginHostServiceListCommandsResponse](#holomush-plugin-v1-PluginHostServiceListCommandsResponse)
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
    - [PluginHostServiceSetSettingRequest](#holomush-plugin-v1-PluginHostServiceSetSettingRequest)
    - [PluginHostServiceSetSettingResponse](#holomush-plugin-v1-PluginHostServiceSetSettingResponse)
    - [QuerySessionStreamsRequest](#holomush-plugin-v1-QuerySessionStreamsRequest)
    - [QuerySessionStreamsResponse](#holomush-plugin-v1-QuerySessionStreamsResponse)
    - [ServiceConfig](#holomush-plugin-v1-ServiceConfig)
    - [ServiceConfig.PluginConfigEntry](#holomush-plugin-v1-ServiceConfig-PluginConfigEntry)
    - [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry)
  
    - [AuditEffect](#holomush-plugin-v1-AuditEffect)
    - [CommandStatus](#holomush-plugin-v1-CommandStatus)
    - [FocusFailureReason](#holomush-plugin-v1-FocusFailureReason)
    - [FocusKind](#holomush-plugin-v1-FocusKind)
    - [SettingScope](#holomush-plugin-v1-SettingScope)
    - [StreamReplayMode](#holomush-plugin-v1-StreamReplayMode)
  
    - [PluginHostService](#holomush-plugin-v1-PluginHostService)
    - [PluginService](#holomush-plugin-v1-PluginService)
  
- [holomush/scene/v1/scene.proto](#holomush_scene_v1_scene-proto)
    - [CastPublishSceneVoteRequest](#holomush-scene-v1-CastPublishSceneVoteRequest)
    - [CastPublishSceneVoteResponse](#holomush-scene-v1-CastPublishSceneVoteResponse)
    - [CastPublishVoteRequest](#holomush-scene-v1-CastPublishVoteRequest)
    - [CastPublishVoteResponse](#holomush-scene-v1-CastPublishVoteResponse)
    - [CharacterSceneInfo](#holomush-scene-v1-CharacterSceneInfo)
    - [CreateSceneRequest](#holomush-scene-v1-CreateSceneRequest)
    - [CreateSceneResponse](#holomush-scene-v1-CreateSceneResponse)
    - [DownloadPublicSceneArchiveRequest](#holomush-scene-v1-DownloadPublicSceneArchiveRequest)
    - [DownloadPublicSceneArchiveResponse](#holomush-scene-v1-DownloadPublicSceneArchiveResponse)
    - [DownloadPublishedSceneRequest](#holomush-scene-v1-DownloadPublishedSceneRequest)
    - [DownloadPublishedSceneResponse](#holomush-scene-v1-DownloadPublishedSceneResponse)
    - [EndSceneRequest](#holomush-scene-v1-EndSceneRequest)
    - [EndSceneResponse](#holomush-scene-v1-EndSceneResponse)
    - [ExportSceneLogRequest](#holomush-scene-v1-ExportSceneLogRequest)
    - [ExportSceneLogResponse](#holomush-scene-v1-ExportSceneLogResponse)
    - [ExtendScenePublishVoteAttemptsRequest](#holomush-scene-v1-ExtendScenePublishVoteAttemptsRequest)
    - [ExtendScenePublishVoteAttemptsResponse](#holomush-scene-v1-ExtendScenePublishVoteAttemptsResponse)
    - [GetPoseOrderRequest](#holomush-scene-v1-GetPoseOrderRequest)
    - [GetPoseOrderResponse](#holomush-scene-v1-GetPoseOrderResponse)
    - [GetPublicSceneArchiveRequest](#holomush-scene-v1-GetPublicSceneArchiveRequest)
    - [GetPublicSceneArchiveResponse](#holomush-scene-v1-GetPublicSceneArchiveResponse)
    - [GetPublishedSceneRequest](#holomush-scene-v1-GetPublishedSceneRequest)
    - [GetPublishedSceneResponse](#holomush-scene-v1-GetPublishedSceneResponse)
    - [GetSceneRequest](#holomush-scene-v1-GetSceneRequest)
    - [GetSceneResponse](#holomush-scene-v1-GetSceneResponse)
    - [InviteToSceneRequest](#holomush-scene-v1-InviteToSceneRequest)
    - [InviteToSceneResponse](#holomush-scene-v1-InviteToSceneResponse)
    - [JoinSceneRequest](#holomush-scene-v1-JoinSceneRequest)
    - [JoinSceneResponse](#holomush-scene-v1-JoinSceneResponse)
    - [KickFromSceneRequest](#holomush-scene-v1-KickFromSceneRequest)
    - [KickFromSceneResponse](#holomush-scene-v1-KickFromSceneResponse)
    - [LeaveSceneRequest](#holomush-scene-v1-LeaveSceneRequest)
    - [LeaveSceneResponse](#holomush-scene-v1-LeaveSceneResponse)
    - [ListCharacterScenesRequest](#holomush-scene-v1-ListCharacterScenesRequest)
    - [ListCharacterScenesResponse](#holomush-scene-v1-ListCharacterScenesResponse)
    - [ListPublishedScenesRequest](#holomush-scene-v1-ListPublishedScenesRequest)
    - [ListPublishedScenesResponse](#holomush-scene-v1-ListPublishedScenesResponse)
    - [ListScenePublishAttemptsRequest](#holomush-scene-v1-ListScenePublishAttemptsRequest)
    - [ListScenePublishAttemptsResponse](#holomush-scene-v1-ListScenePublishAttemptsResponse)
    - [ListScenesRequest](#holomush-scene-v1-ListScenesRequest)
    - [ListScenesResponse](#holomush-scene-v1-ListScenesResponse)
    - [ParticipantInfo](#holomush-scene-v1-ParticipantInfo)
    - [PauseSceneRequest](#holomush-scene-v1-PauseSceneRequest)
    - [PauseSceneResponse](#holomush-scene-v1-PauseSceneResponse)
    - [PoseOrderEntry](#holomush-scene-v1-PoseOrderEntry)
    - [PublicSceneArchive](#holomush-scene-v1-PublicSceneArchive)
    - [PublishedSceneEntry](#holomush-scene-v1-PublishedSceneEntry)
    - [PublishedSceneSummary](#holomush-scene-v1-PublishedSceneSummary)
    - [PublishedSceneVoteSummary](#holomush-scene-v1-PublishedSceneVoteSummary)
    - [ResumeSceneRequest](#holomush-scene-v1-ResumeSceneRequest)
    - [ResumeSceneResponse](#holomush-scene-v1-ResumeSceneResponse)
    - [SceneInfo](#holomush-scene-v1-SceneInfo)
    - [ScenePublishCoolOffStartedEvent](#holomush-scene-v1-ScenePublishCoolOffStartedEvent)
    - [ScenePublishResolvedEvent](#holomush-scene-v1-ScenePublishResolvedEvent)
    - [ScenePublishStartedEvent](#holomush-scene-v1-ScenePublishStartedEvent)
    - [ScenePublishVoteAttemptsExtendedEvent](#holomush-scene-v1-ScenePublishVoteAttemptsExtendedEvent)
    - [ScenePublishVoteCastEvent](#holomush-scene-v1-ScenePublishVoteCastEvent)
    - [ScenePublishWithdrawnEvent](#holomush-scene-v1-ScenePublishWithdrawnEvent)
    - [StartScenePublishRequest](#holomush-scene-v1-StartScenePublishRequest)
    - [StartScenePublishResponse](#holomush-scene-v1-StartScenePublishResponse)
    - [TransferOwnershipRequest](#holomush-scene-v1-TransferOwnershipRequest)
    - [TransferOwnershipResponse](#holomush-scene-v1-TransferOwnershipResponse)
    - [UpdateSceneRequest](#holomush-scene-v1-UpdateSceneRequest)
    - [UpdateSceneResponse](#holomush-scene-v1-UpdateSceneResponse)
    - [WatchSceneRequest](#holomush-scene-v1-WatchSceneRequest)
    - [WatchSceneResponse](#holomush-scene-v1-WatchSceneResponse)
    - [WithdrawScenePublishRequest](#holomush-scene-v1-WithdrawScenePublishRequest)
    - [WithdrawScenePublishResponse](#holomush-scene-v1-WithdrawScenePublishResponse)
  
    - [SceneService](#holomush-scene-v1-SceneService)
  
- [holomush/sceneaccess/v1/sceneaccess.proto](#holomush_sceneaccess_v1_sceneaccess-proto)
    - [DownloadPublicSceneArchiveRequest](#holomush-sceneaccess-v1-DownloadPublicSceneArchiveRequest)
    - [DownloadPublicSceneArchiveResponse](#holomush-sceneaccess-v1-DownloadPublicSceneArchiveResponse)
    - [ExportSceneRequest](#holomush-sceneaccess-v1-ExportSceneRequest)
    - [ExportSceneResponse](#holomush-sceneaccess-v1-ExportSceneResponse)
    - [GetPublicSceneArchiveRequest](#holomush-sceneaccess-v1-GetPublicSceneArchiveRequest)
    - [GetPublicSceneArchiveResponse](#holomush-sceneaccess-v1-GetPublicSceneArchiveResponse)
    - [GetSceneForViewerRequest](#holomush-sceneaccess-v1-GetSceneForViewerRequest)
    - [GetSceneForViewerResponse](#holomush-sceneaccess-v1-GetSceneForViewerResponse)
    - [ListMyScenesRequest](#holomush-sceneaccess-v1-ListMyScenesRequest)
    - [ListMyScenesResponse](#holomush-sceneaccess-v1-ListMyScenesResponse)
    - [ListPublishedScenesRequest](#holomush-sceneaccess-v1-ListPublishedScenesRequest)
    - [ListPublishedScenesResponse](#holomush-sceneaccess-v1-ListPublishedScenesResponse)
    - [ListScenesForViewerRequest](#holomush-sceneaccess-v1-ListScenesForViewerRequest)
    - [ListScenesForViewerResponse](#holomush-sceneaccess-v1-ListScenesForViewerResponse)
    - [SetSceneFocusRequest](#holomush-sceneaccess-v1-SetSceneFocusRequest)
    - [SetSceneFocusResponse](#holomush-sceneaccess-v1-SetSceneFocusResponse)
    - [WatchSceneRequest](#holomush-sceneaccess-v1-WatchSceneRequest)
    - [WatchSceneResponse](#holomush-sceneaccess-v1-WatchSceneResponse)
  
    - [SceneAccessService](#holomush-sceneaccess-v1-SceneAccessService)
  
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
    - [WebAvailableCommand](#holomush-web-v1-WebAvailableCommand)
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
    - [WebListCommandsRequest](#holomush-web-v1-WebListCommandsRequest)
    - [WebListCommandsResponse](#holomush-web-v1-WebListCommandsResponse)
    - [WebListCommandsResponse.AliasesEntry](#holomush-web-v1-WebListCommandsResponse-AliasesEntry)
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
  
- [holomush/world/v1/world.proto](#holomush_world_v1_world-proto)
    - [CharacterInfo](#holomush-world-v1-CharacterInfo)
    - [ExitInfo](#holomush-world-v1-ExitInfo)
    - [GetCharacterRequest](#holomush-world-v1-GetCharacterRequest)
    - [GetCharacterResponse](#holomush-world-v1-GetCharacterResponse)
    - [GetLocationRequest](#holomush-world-v1-GetLocationRequest)
    - [GetLocationResponse](#holomush-world-v1-GetLocationResponse)
    - [ListCharactersAtLocationRequest](#holomush-world-v1-ListCharactersAtLocationRequest)
    - [ListCharactersAtLocationResponse](#holomush-world-v1-ListCharactersAtLocationResponse)
    - [ListExitsRequest](#holomush-world-v1-ListExitsRequest)
    - [ListExitsResponse](#holomush-world-v1-ListExitsResponse)
    - [LocationInfo](#holomush-world-v1-LocationInfo)
  
    - [WorldService](#holomush-world-v1-WorldService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="holomush_core_v1_core-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/core/v1/core.proto



<a name="holomush-core-v1-AuthenticatePlayerRequest"></a>

### AuthenticatePlayerRequest
AuthenticatePlayerRequest carries phase-one login credentials.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  | username identifies the player account. |
| password | [string](#string) |  | password is the plaintext password to verify (over the secured transport). |
| captcha_token | [string](#string) |  | captcha_token is an optional anti-automation token. |
| remember_me | [bool](#bool) |  | remember_me requests a longer-lived session per the gateway&#39;s cookie policy. |






<a name="holomush-core-v1-AuthenticatePlayerResponse"></a>

### AuthenticatePlayerResponse
AuthenticatePlayerResponse returns the minted player session token and the
roster needed to drive phase-two character selection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when credentials verified. |
| player_session_token | [string](#string) |  | player_session_token is the bearer token for subsequent post-auth RPCs; present only on success. |
| error_message | [string](#string) |  | error_message is a sanitized, generic failure message (&#34;invalid username or password&#34;) on failure. |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated | characters is the player&#39;s roster for the character-select screen. |
| default_character_id | [string](#string) |  | default_character_id is the player&#39;s preferred character to pre-select, if set. |
| session_ttl_seconds | [int64](#int64) |  | session_ttl_seconds is the session lifetime in seconds. The web gateway uses it to set the cookie MaxAge so the cookie expires with the underlying session (preventing stale cookies outliving short guest sessions). |






<a name="holomush-core-v1-AvailableCommand"></a>

### AvailableCommand
AvailableCommand is one command&#39;s metadata in a ListAvailableCommands result.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the canonical command name. |
| help | [string](#string) |  | help is the one-line description. |
| usage | [string](#string) |  | usage is the usage pattern. |
| source | [string](#string) |  | source is &#34;core&#34; or the owning plugin name. |






<a name="holomush-core-v1-CharacterSummary"></a>

### CharacterSummary
CharacterSummary is the roster view of one character: enough to render a
character-select screen, enriched with live session status and last location.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the character&#39;s ULID. |
| character_name | [string](#string) |  | character_name is the character&#39;s display name. |
| has_active_session | [bool](#bool) |  | has_active_session is true when this character has a session in the Active state right now. |
| session_status | [string](#string) |  | session_status is the string form of the character&#39;s current session status (e.g. &#34;active&#34;, &#34;detached&#34;); empty when no session exists. |
| last_location | [string](#string) |  | last_location is the resolved name of the character&#39;s last-known location; empty when unknown or unresolvable. |
| last_played_at | [int64](#int64) |  | last_played_at is an epoch timestamp of last play (unset/zero when never played). |






<a name="holomush-core-v1-CheckPlayerSessionRequest"></a>

### CheckPlayerSessionRequest
CheckPlayerSessionRequest validates a session token, typically the value from
a web auth cookie.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token is the token to validate. |






<a name="holomush-core-v1-CheckPlayerSessionResponse"></a>

### CheckPlayerSessionResponse
CheckPlayerSessionResponse returns the player identity behind a valid token.
The failure path returns an Unauthenticated status with no body, so these
fields are absent for unknown/expired sessions — preserving the enumeration-
safety contract documented in internal/auth (session ownership).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  | player_name is the account username. |
| player_id | [string](#string) |  | player_id is the player&#39;s ULID. |
| is_guest | [bool](#bool) |  | is_guest is true when the session belongs to an ephemeral guest player. |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated | characters is the player&#39;s roster (enriched with session status). |






<a name="holomush-core-v1-ConfirmPasswordResetRequest"></a>

### ConfirmPasswordResetRequest
ConfirmPasswordResetRequest completes a reset using the emailed token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | token is the single-use reset token from the reset email. |
| new_password | [string](#string) |  | new_password is the plaintext replacement password. |






<a name="holomush-core-v1-ConfirmPasswordResetResponse"></a>

### ConfirmPasswordResetResponse
ConfirmPasswordResetResponse reports the outcome with a sanitized error.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the password was reset. |
| error_message | [string](#string) |  | error_message is a sanitized failure message on failure (never echoes the token). |






<a name="holomush-core-v1-ControlFrame"></a>

### ControlFrame
ControlFrame is a non-event control message delivered on the Subscribe stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-core-v1-ControlSignal) |  | signal classifies the control message. |
| message | [string](#string) |  | message is optional human-readable context for the signal. |
| attach_moment_ms | [int64](#int64) |  | attach_moment_ms is the server&#39;s wall-clock epoch-ms at the moment the Subscribe handler attached its durable consumer. It is carried ONLY on CONTROL_SIGNAL_REPLAY_COMPLETE; clients reading other signals MUST ignore it. The client passes this value as not_after_ms on subsequent backfill (QueryStreamHistory) calls so backfill returns ONLY events with timestamp &lt;= attach_moment_ms — eliminating the race where a post-attach event could appear both as a dimmed backfill row and a live Subscribe delivery. It is 0 on legacy servers; clients MUST treat 0 as &#34;no upper bound&#34; (back-compat). |
| scene_id | [string](#string) |  | scene_id identifies the scene that produced a SCENE_ACTIVITY signal; the bare scene ULID (not a subject). Set ONLY on CONTROL_SIGNAL_SCENE_ACTIVITY; clients reading other signals MUST ignore it. |






<a name="holomush-core-v1-CreateCharacterRequest"></a>

### CreateCharacterRequest
CreateCharacterRequest adds a character to the authenticated player&#39;s roster.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token proves the caller&#39;s authenticated player identity. |
| character_name | [string](#string) |  | character_name is the desired name for the new character. |






<a name="holomush-core-v1-CreateCharacterResponse"></a>

### CreateCharacterResponse
CreateCharacterResponse returns the newly created character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the character was created. |
| character_id | [string](#string) |  | character_id is the new character&#39;s ULID. |
| character_name | [string](#string) |  | character_name is the new character&#39;s name as stored. |
| error_message | [string](#string) |  | error_message is a sanitized failure message on failure. |






<a name="holomush-core-v1-CreateGuestRequest"></a>

### CreateGuestRequest
CreateGuestRequest is empty: a guest provisioning takes no parameters.






<a name="holomush-core-v1-CreateGuestResponse"></a>

### CreateGuestResponse
CreateGuestResponse returns an ephemeral guest player session plus the starter
character that was provisioned alongside it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the guest was provisioned. |
| error_message | [string](#string) |  | error_message is a generic failure message on failure. |
| player_session_token | [string](#string) |  | player_session_token is the bearer token for the guest session. |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated | characters holds the single starter character provisioned for the guest. |
| default_character_id | [string](#string) |  | default_character_id is the starter character to pre-select. |
| session_ttl_seconds | [int64](#int64) |  | session_ttl_seconds is the session lifetime in seconds (see AuthenticatePlayerResponse). For guest sessions this is the shorter guest TTL, not the regular-player TTL. |






<a name="holomush-core-v1-CreatePlayerRequest"></a>

### CreatePlayerRequest
CreatePlayerRequest carries new-account registration details.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  | username is the desired account name. |
| password | [string](#string) |  | password is the desired plaintext password. |
| email | [string](#string) |  | email is the contact email for the account (used by password reset). |
| captcha_token | [string](#string) |  | captcha_token is an optional anti-automation token. |






<a name="holomush-core-v1-CreatePlayerResponse"></a>

### CreatePlayerResponse
CreatePlayerResponse returns the new account&#39;s session token; the new player
is logged in immediately but has an empty character roster.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the account was created. |
| player_session_token | [string](#string) |  | player_session_token is the bearer token for the newly created, logged-in player. |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated | characters is always empty for a freshly created player. |
| error_message | [string](#string) |  | error_message is a sanitized failure message on failure. |
| session_ttl_seconds | [int64](#int64) |  | session_ttl_seconds is the session lifetime in seconds (see AuthenticatePlayerResponse). |






<a name="holomush-core-v1-DisconnectRequest"></a>

### DisconnectRequest
DisconnectRequest detaches a connection, or the whole session, from the game.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the session to disconnect. |
| connection_id | [string](#string) |  | connection_id, when set, removes only that specific connection; empty disconnects the session as a whole. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. It must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |






<a name="holomush-core-v1-DisconnectResponse"></a>

### DisconnectResponse
DisconnectResponse reports the outcome. Disconnect is idempotent: a session
that is already gone returns success.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |
| success | [bool](#bool) |  | success is true on a completed (or already-complete) disconnect. |






<a name="holomush-core-v1-EventFrame"></a>

### EventFrame
EventFrame is one delivered game event. The same shape is produced by both
the live Subscribe path and the QueryStreamHistory backfill path.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the event&#39;s ULID — its identity and dedup key, NOT its ordering key. |
| stream | [string](#string) |  | stream is the fully-qualified JetStream subject the event belongs to (e.g. &#34;events.main.location.&lt;ULID&gt;&#34;). Producers and clients exchange domain-relative dot references (e.g. &#34;location.&lt;ULID&gt;&#34;); the server qualifies them on the way in, so delivered frames carry the qualified form. |
| type | [string](#string) |  | type is the event type string (e.g. say, pose, command_response). |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | timestamp is the server-stamped event time. |
| actor_type | [string](#string) |  | actor_type names the kind of actor that produced the event (character, plugin, etc.). |
| actor_id | [string](#string) |  | actor_id identifies the specific actor that produced the event. |
| payload | [bytes](#bytes) |  | payload is the type-specific event body, opaque at this layer. Empty when metadata_only is true. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor for this event. The server populates it on QueryStreamHistory responses and Subscribe deliveries so clients can resume without re-delivering events they already processed. |
| rendering | [RenderingMetadata](#holomush-core-v1-RenderingMetadata) |  | rendering is the cleartext rendering band, populated by RenderingPublisher at emit time. It MUST be present on every frame this server produces (INV-EVENTBUS-2); the gateway treats absence as a contract violation (drops &#43; metric &#43; log per INV-EVENTBUS-6). |
| metadata_only | [bool](#bool) |  | metadata_only flags a delivery whose plaintext was withheld by the host&#39;s AuthGuard (Phase 3b decrypt path). When true, payload is empty bytes and the recipient was either not in the DEK&#39;s participant set, lacked the requisite plugin manifest declaration / ABAC grant, or hit the audit-emit backpressure throttle. It is false on every legitimate delivery (including legitimately empty-payload events such as a presence event with no content). Set by the Subscribe / QueryStreamHistory handler at fan-out time; NEVER set by emitters and NEVER persisted to events_audit (storage rows always carry the sender&#39;s payload, ciphertext or cleartext). |
| no_plaintext_reason | [NoPlaintextReason](#holomush-core-v1-NoPlaintextReason) |  | no_plaintext_reason classifies why metadata_only=true was stamped. It is UNSPECIFIED on metadata_only=false deliveries and one of the typed reasons when metadata_only=true. |






<a name="holomush-core-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest
GetCommandHistoryRequest asks for the recent command lines recorded for a
session (the per-session command ring buffer, not event history).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the session whose command history is requested. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. It must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |






<a name="holomush-core-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse
GetCommandHistoryResponse returns the recorded command lines.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |
| success | [bool](#bool) |  | success is true when history was retrieved. |
| commands | [string](#string) | repeated | commands lists the recent command lines, oldest-to-newest within the ring. |
| error | [string](#string) |  | error carries a failure message when success is false. |






<a name="holomush-core-v1-HandleCommandRequest"></a>

### HandleCommandRequest
HandleCommandRequest carries one player-issued command to dispatch within the
caller&#39;s game session.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data (see RequestMeta). |
| session_id | [string](#string) |  | session_id names the game session in whose context the command runs. |
| command | [string](#string) |  | command is the raw command line as typed by the player; the dispatcher parses and routes it. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. Required for all post-auth RPCs. It must match the player_id of session_id or the request is rejected with SESSION_NOT_FOUND. |
| connection_id | [string](#string) |  | connection_id is the ULID of the originating gateway connection (Phase 5). Populated by telnet and web gateways; empty for non-gateway callers. The server uses it to route scene-focus autofocus to the correct connection (T20-T23). An empty string is accepted (parsed as the zero ULID). |






<a name="holomush-core-v1-HandleCommandResponse"></a>

### HandleCommandResponse
HandleCommandResponse reports only whether dispatch succeeded. All player-
visible command output is delivered out of band as command_response events on
the character&#39;s stream, not in this reply.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data back to the caller. |
| success | [bool](#bool) |  | success is true when the command dispatched without a transport/ownership error. User-facing command errors are still reported via command_response events with success=true here. |
| error | [string](#string) |  | error carries a transport/ownership failure message when success is false. |






<a name="holomush-core-v1-ListAvailableCommandsRequest"></a>

### ListAvailableCommandsRequest
ListAvailableCommandsRequest asks for the session character&#39;s executable set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id; failures collapse to SESSION_NOT_FOUND. |
| session_id | [string](#string) |  | session_id names the session whose character&#39;s command set is enumerated. |






<a name="holomush-core-v1-ListAvailableCommandsResponse"></a>

### ListAvailableCommandsResponse
ListAvailableCommandsResponse returns the filtered set &#43; alias map.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |
| commands | [AvailableCommand](#holomush-core-v1-AvailableCommand) | repeated | commands is the ABAC-filtered set the session character may execute. |
| aliases | [ListAvailableCommandsResponse.AliasesEntry](#holomush-core-v1-ListAvailableCommandsResponse-AliasesEntry) | repeated | aliases maps alias → canonical command name (system/manifest aliases for visible commands). |
| incomplete | [bool](#bool) |  | incomplete is true when engine errors hid some commands. |






<a name="holomush-core-v1-ListAvailableCommandsResponse-AliasesEntry"></a>

### ListAvailableCommandsResponse.AliasesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-core-v1-ListCharactersRequest"></a>

### ListCharactersRequest
ListCharactersRequest asks for the authenticated player&#39;s character roster.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token proves the caller&#39;s authenticated player identity. |






<a name="holomush-core-v1-ListCharactersResponse"></a>

### ListCharactersResponse
ListCharactersResponse returns the player&#39;s roster with session-status enrichment.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated | characters is the player&#39;s roster. |






<a name="holomush-core-v1-ListFocusPresenceRequest"></a>

### ListFocusPresenceRequest
ListFocusPresenceRequest asks for the current-state presence snapshot of the
session&#39;s focus context.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id; failures collapse to SESSION_NOT_FOUND. |
| session_id | [string](#string) |  | session_id names the session whose focus context is queried. |






<a name="holomush-core-v1-ListFocusPresenceResponse"></a>

### ListFocusPresenceResponse
ListFocusPresenceResponse returns the presence snapshot. For a session with no
location yet, entries is empty under the LOCATION context rather than an error.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |
| context | [PresenceContext](#holomush-core-v1-PresenceContext) |  | context names the focus context the snapshot describes (LOCATION today). |
| context_id | [string](#string) |  | context_id is the identifier of the context: a location_id for LOCATION (and a scene_id for the future SCENE context). |
| entries | [PresenceEntry](#holomush-core-v1-PresenceEntry) | repeated | entries is the deduplicated set of characters present in the context. |






<a name="holomush-core-v1-ListPlayerSessionsRequest"></a>

### ListPlayerSessionsRequest
ListPlayerSessionsRequest asks for the caller&#39;s own active PlayerSessions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token identifies the caller; the response lists that player&#39;s sessions. |






<a name="holomush-core-v1-ListPlayerSessionsResponse"></a>

### ListPlayerSessionsResponse
ListPlayerSessionsResponse returns the caller&#39;s PlayerSessions. An empty list
is also the enumeration-safe response on any auth failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [PlayerSessionInfo](#holomush-core-v1-PlayerSessionInfo) | repeated | sessions is the caller&#39;s active PlayerSessions; never includes tokens. |






<a name="holomush-core-v1-ListSessionStreamsRequest"></a>

### ListSessionStreamsRequest
ListSessionStreamsRequest asks which streams a session is currently subscribed
to.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the session whose subscribed streams are listed. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id; failures collapse to SESSION_NOT_FOUND (closing the stream-enumeration IDOR). |






<a name="holomush-core-v1-ListSessionStreamsResponse"></a>

### ListSessionStreamsResponse
ListSessionStreamsResponse returns the session&#39;s subscribed stream names.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| streams | [string](#string) | repeated | streams lists the subscribed stream names as domain-relative dot references (e.g. &#34;character.&lt;ULID&gt;&#34;, &#34;location.&lt;ULID&gt;&#34;, plugin streams) — the form the client passes back to Subscribe/QueryStreamHistory unchanged, which the server qualifies. Delivered EventFrames carry the fully-qualified subject (see EventFrame.stream), not this relative form. |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |






<a name="holomush-core-v1-LogoutRequest"></a>

### LogoutRequest
LogoutRequest ends the player session identified by the supplied token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token identifies the PlayerSession to end. |






<a name="holomush-core-v1-LogoutResponse"></a>

### LogoutResponse
LogoutResponse is empty: logout reports success solely by returning without an
error status.






<a name="holomush-core-v1-PlayerSessionInfo"></a>

### PlayerSessionInfo
PlayerSessionInfo describes one of the caller&#39;s PlayerSessions for device-
management UX. It never carries the session token — only safe-to-display
metadata.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the PlayerSession&#39;s ULID. Safe to show the user — this is a resource handle, not a secret — and is the value passed as target_session_id to RevokePlayerSession. |
| created_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | created_at is when the session was established. |
| last_active | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | last_active is sourced from player_sessions.updated_at, bumped whenever the session is refreshed. |
| user_agent | [string](#string) |  | user_agent is the client user-agent recorded at session creation. |
| ip_address | [string](#string) |  | ip_address is the client IP recorded at session creation. |
| is_current | [bool](#bool) |  | is_current is true for exactly the PlayerSession that made the ListPlayerSessions request — supports a &#34;this device&#34; indicator. |






<a name="holomush-core-v1-PresenceEntry"></a>

### PresenceEntry
PresenceEntry describes one character present in a focus context.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the present character&#39;s ULID. |
| character_name | [string](#string) |  | character_name is the resolved display name; entries whose name cannot be resolved are dropped rather than returned empty. |
| state | [PresenceState](#holomush-core-v1-PresenceState) |  | state is the character&#39;s presence state (ACTIVE for the location resolver). |






<a name="holomush-core-v1-QueryStreamHistoryRequest"></a>

### QueryStreamHistoryRequest
QueryStreamHistoryRequest reads a page of event history from one stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the requesting session; its identity drives authorization. |
| stream | [string](#string) |  | stream is the domain-relative dot reference whose history is read (e.g. &#34;location.&lt;ULID&gt;&#34;, &#34;character.&lt;ULID&gt;&#34;); the server qualifies it to the fully-qualified JetStream subject before authorization and the bus fetch. |
| count | [int32](#int32) |  | count is the requested page size. 0 selects the server default (150); the server caps it at 500; a negative value is rejected with INVALID_ARGUMENT. |
| not_before_ms | [int64](#int64) |  | not_before_ms is an epoch-ms time floor; 0 means no lower bound. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous response. Events older than the cursor position are returned; empty starts from the latest. |
| not_after_ms | [int64](#int64) |  | not_after_ms is an epoch-ms time ceiling; 0 means no upper bound (back-compat). INCLUSIVE: events with timestamp == not_after_ms are returned. The web client sets it from ControlFrame.attach_moment_ms at connect time to bound backfill to events that existed before the Subscribe stream attached, eliminating the connect-time race where a user-emitted event could appear both as a dimmed backfill row and a live Subscribe delivery. |






<a name="holomush-core-v1-QueryStreamHistoryResponse"></a>

### QueryStreamHistoryResponse
QueryStreamHistoryResponse returns one page of history plus pagination state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta echoes request correlation data. |
| events | [EventFrame](#holomush-core-v1-EventFrame) | repeated | events is the page of history frames, newest-first within the page. |
| has_more | [bool](#bool) |  | has_more is true when older events remain beyond this page. |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next (older) page; empty when has_more is false. |






<a name="holomush-core-v1-RefreshConnectionRequest"></a>

### RefreshConnectionRequest
RefreshConnectionRequest asks core to bump the lease for one connection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the game session owning the connection. |
| connection_id | [string](#string) |  | connection_id is the connection whose lease to refresh. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. |






<a name="holomush-core-v1-RefreshConnectionResponse"></a>

### RefreshConnectionResponse
RefreshConnectionResponse is empty on success; failures are gRPC status codes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  | meta carries response correlation data. |






<a name="holomush-core-v1-RenderingMetadata"></a>

### RenderingMetadata
RenderingMetadata carries cleartext rendering instructions for an event. It is
populated by RenderingPublisher.Publish at emit time from the verb registry —
one schema with two transports (the gRPC Subscribe EventFrame and the
JetStream envelope). See
docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| category | [string](#string) |  | category drives client-side renderer routing and must be non-empty. |
| format | [string](#string) |  | format drives within-category presentation and must be non-empty. |
| label | [string](#string) |  | label provides type-specific display text. Required when format == &#34;speech&#34;. |
| display_target | [EventChannel](#holomush-core-v1-EventChannel) |  | display_target routes the event to TERMINAL, STATE, or BOTH on the client. It must be a defined, non-zero EventChannel. |
| source_plugin | [string](#string) |  | source_plugin names the plugin that owns this event type, or &#34;builtin&#34; for host-owned types. Recorded for historical/audit fidelity. |
| source_plugin_version | [string](#string) |  | source_plugin_version is the manifest&#39;s version field, or &#34;host-&lt;binary version&gt;&#34; for builtins. Recorded for historical/audit fidelity. |






<a name="holomush-core-v1-RequestMeta"></a>

### RequestMeta
RequestMeta travels on every request so the server can correlate a single
RPC across logs, traces, and audit. The CoreServer handlers read meta.request_id
into the slog &#34;request_id&#34; field and emit it as an OTel span attribute.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | request_id is a client-supplied ULID used only for log/trace correlation. It is not an identity or ownership token; an empty value is accepted and simply suppresses the per-request correlation attribute. |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | timestamp records when the client issued the request. Advisory only — the server does not gate on it. |






<a name="holomush-core-v1-RequestPasswordResetRequest"></a>

### RequestPasswordResetRequest
RequestPasswordResetRequest begins a password-reset flow by email.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  | email is the account email to send the reset to. |






<a name="holomush-core-v1-RequestPasswordResetResponse"></a>

### RequestPasswordResetResponse
RequestPasswordResetResponse always reports success regardless of whether the
email exists — an intentional account-enumeration-prevention measure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is always true (enumeration-safe; reveals nothing about whether the email is registered). |






<a name="holomush-core-v1-ResponseMeta"></a>

### ResponseMeta
ResponseMeta is the response-side counterpart to RequestMeta. CoreServer
echoes the originating request_id back via responseMeta() so a client can
match an asynchronous-feeling reply to the call that produced it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | request_id is the value echoed from the originating RequestMeta.request_id. |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | timestamp records when the server produced the response. |






<a name="holomush-core-v1-RevokeOtherPlayerSessionsRequest"></a>

### RevokeOtherPlayerSessionsRequest
RevokeOtherPlayerSessionsRequest bulk-revokes the caller&#39;s other sessions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token identifies the caller; the current session is preserved and all others are revoked. |






<a name="holomush-core-v1-RevokeOtherPlayerSessionsResponse"></a>

### RevokeOtherPlayerSessionsResponse
RevokeOtherPlayerSessionsResponse reports how many sessions were revoked.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the bulk revoke completed. |
| revoked_count | [int32](#int32) |  | revoked_count is the number of PlayerSessions deleted (excluding the current one). |






<a name="holomush-core-v1-RevokePlayerSessionRequest"></a>

### RevokePlayerSessionRequest
RevokePlayerSessionRequest deletes one of the caller&#39;s PlayerSessions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token identifies the caller; only the caller&#39;s own sessions may be revoked. |
| target_session_id | [string](#string) |  | target_session_id is the PlayerSession.id (ULID) to revoke — NOT the game session_id. A revoke targeting another player&#39;s session collapses to &#34;session not found&#34;. |






<a name="holomush-core-v1-RevokePlayerSessionResponse"></a>

### RevokePlayerSessionResponse
RevokePlayerSessionResponse reports the outcome with an enumeration-safe error.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the target session was deleted. |
| error_message | [string](#string) |  | error_message is &#34;session not found&#34; on any failure, including cross-player attempts (enumeration-safe). |






<a name="holomush-core-v1-SelectCharacterRequest"></a>

### SelectCharacterRequest
SelectCharacterRequest carries phase-two character selection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  | player_session_token proves the caller&#39;s authenticated player identity. |
| character_id | [string](#string) |  | character_id names the character to enter the game as; it must belong to the authenticated player. |
| client_type | [string](#string) |  | client_type declares the surface establishing the session (terminal/comms_hub/telnet — the session_connections vocabulary). When &#34;comms_hub&#34;, a FRESH session creation skips the grid arrive emission: scenes-workspace sessions must not announce the character on the grid (spec 2026-06-07 §V2). Empty preserves the legacy behavior (arrive). Reattach paths never re-emit arrive regardless of this field. |






<a name="holomush-core-v1-SelectCharacterResponse"></a>

### SelectCharacterResponse
SelectCharacterResponse returns the game session created or reattached for the
chosen character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when a game session was established. |
| session_id | [string](#string) |  | session_id is the game session id to use for Subscribe/HandleCommand. |
| character_name | [string](#string) |  | character_name is the selected character&#39;s display name. |
| reattached | [bool](#bool) |  | reattached is true when an existing detached session was resumed (preserving scrollback) rather than a new one created. |
| error_message | [string](#string) |  | error_message is a sanitized failure message on failure. |






<a name="holomush-core-v1-SubscribeRequest"></a>

### SubscribeRequest
SubscribeRequest opens the per-session event stream. The server, not the
client, decides which streams to deliver and the replay policy.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  | meta carries request correlation data. |
| session_id | [string](#string) |  | session_id names the game session whose events are streamed. |
| player_session_token | [string](#string) |  | player_session_token proves the caller owns session_id. |
| connection_id | [string](#string) |  | connection_id identifies this specific client attachment. The gateway generates a fresh ULID per stream. Required so core can register and deregister the connection atomically with the stream lifecycle. When set, client_type must also be set or the request is rejected. |
| client_type | [string](#string) |  | client_type describes the connecting client for observability and routing: &#34;terminal&#34;, &#34;telnet&#34;, or future client types. |






<a name="holomush-core-v1-SubscribeResponse"></a>

### SubscribeResponse
SubscribeResponse is one item on the Subscribe stream: either a game event or
a control frame.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [EventFrame](#holomush-core-v1-EventFrame) |  | event carries one delivered game event. |
| control | [ControlFrame](#holomush-core-v1-ControlFrame) |  | control carries an out-of-band control signal (e.g. replay-complete). |





 


<a name="holomush-core-v1-ControlSignal"></a>

### ControlSignal
ControlSignal classifies an out-of-band control frame interleaved into the
Subscribe stream alongside event frames.

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 | CONTROL_SIGNAL_UNSPECIFIED is the zero value; never sent. |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 | CONTROL_SIGNAL_REPLAY_COMPLETE marks the boundary between replayed history and live deliveries on a Subscribe stream. |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 | CONTROL_SIGNAL_STREAM_CLOSED tells the client the server is ending the stream (e.g. the session was disconnected or booted). |
| CONTROL_SIGNAL_SCENE_ACTIVITY | 3 | CONTROL_SIGNAL_SCENE_ACTIVITY notifies the client that a scene it is a member of received an event while this connection was NOT focused on it. Carries scene_id only — never event content (the payload may be encrypted; the ping requires no decryption). Drives workspace unread badges; lossy by design (clients re-sync via ListMyScenes snapshots). |



<a name="holomush-core-v1-EventChannel"></a>

### EventChannel
EventChannel identifies the destination channel for event delivery. This is
the canonical internal definition; webv1.EventChannel is kept in lockstep for
the web wire format (INV-EVENTBUS-16).

| Name | Number | Description |
| ---- | ------ | ----------- |
| EVENT_CHANNEL_UNSPECIFIED | 0 | EVENT_CHANNEL_UNSPECIFIED is the zero value; rendering metadata validation rejects it (display_target must be a defined non-zero channel). |
| EVENT_CHANNEL_TERMINAL | 1 | EVENT_CHANNEL_TERMINAL routes the event to the scrolling text terminal surface. |
| EVENT_CHANNEL_STATE | 2 | EVENT_CHANNEL_STATE routes the event to the client&#39;s structured state surface (e.g. presence / status panels) rather than the terminal. |
| EVENT_CHANNEL_BOTH | 3 | EVENT_CHANNEL_BOTH routes the event to both the terminal and the state surface. |
| EVENT_CHANNEL_AUDIT_ONLY | 4 | EVENT_CHANNEL_AUDIT_ONLY tags host-emit security/audit events that MUST persist to events_audit but MUST NOT be delivered to client surfaces (telnet, web). The gRPC Subscribe handler drops these before send; the audit projection persists them like any other event. Used by crypto.totp_*, crypto.policy_set, and similar host-emitted audit types. |



<a name="holomush-core-v1-NoPlaintextReason"></a>

### NoPlaintextReason
NoPlaintextReason enumerates the causes for a metadata_only=true delivery so
clients can distinguish, for example, a destroyed/stale DEK from an
authorization denial or backpressure-driven withholding. The Go-side mirror
is internal/eventbus.NoPlaintextReason (a 1:1 bijection, asserted by
types_proto_sync_test.go); the reasons map to wire strings in the eventbus
history readback layer. This enum backs the EventFrame / read-stream path
only — the separate plugin own-audit-row decrypt path (RowResult in
audit.proto) uses string reasons and adds a &#34;not_owner&#34; value that has no
counterpart here.

| Name | Number | Description |
| ---- | ------ | ----------- |
| NO_PLAINTEXT_REASON_UNSPECIFIED | 0 | NO_PLAINTEXT_REASON_UNSPECIFIED is the zero value and MUST hold when metadata_only=false. A client that sees it together with metadata_only=true MUST treat the delivery as a contract violation (host stamped without classifying). |
| NO_PLAINTEXT_REASON_AUTHGUARD_DENY | 1 | NO_PLAINTEXT_REASON_AUTHGUARD_DENY means the recipient was not in the DEK&#39;s participant set or lacked the requisite plugin manifest declaration / ABAC grant. Phase 3b AuthGuard deny. |
| NO_PLAINTEXT_REASON_STALE_DEK | 2 | NO_PLAINTEXT_REASON_STALE_DEK means both the hot and cold tier DEKs were indecipherable — a production-real outcome after a sub-epic E rekey plus DEK destruction (INV-CRYPTO-108 double miss). |
| NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL | 3 | NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL means a plugin audit-emit hit backpressure (queue full) — a host-side TOCTOU defense. |
| NO_PLAINTEXT_REASON_DEK_MISSING | 4 | NO_PLAINTEXT_REASON_DEK_MISSING means the cold-tier audit row had no dek_ref (DEK reference column missing or NULL). Stamped exclusively by sub-epic F&#39;s operator-read classifier (INV-CRYPTO-66). |
| NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS | 5 | NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS means a cold-tier audit row references a DEK whose column set does not match the event&#39;s AAD declaration. Stamped exclusively by sub-epic F&#39;s classifier. |
| NO_PLAINTEXT_REASON_INTERNAL | 6 | NO_PLAINTEXT_REASON_INTERNAL is the catch-all for unexpected decrypt failures not covered by the specific cases above. Stamped exclusively by sub-epic F&#39;s classifier. |
| NO_PLAINTEXT_REASON_DOWNGRADE_REFUSED | 7 | NO_PLAINTEXT_REASON_DOWNGRADE_REFUSED is a Phase 7 PluginDowngradeFence layer (1) refusal — the host&#39;s read-side fence rejected the row before decrypt, either because the type is in the always-sensitive manifest set and the plugin returned an identity codec (INV-CRYPTO-42), or because the dek_ref is unknown / absent for a non-identity codec (INV-CRYPTO-50). The original event_id is preserved; payload is empty per master INV-CRYPTO-15. |



<a name="holomush-core-v1-PresenceContext"></a>

### PresenceContext
PresenceContext names the kind of focus context a presence snapshot describes,
returned in ListFocusPresenceResponse.

| Name | Number | Description |
| ---- | ------ | ----------- |
| PRESENCE_CONTEXT_UNSPECIFIED | 0 | PRESENCE_CONTEXT_UNSPECIFIED is the zero value; not returned on success. |
| PRESENCE_CONTEXT_LOCATION | 1 | PRESENCE_CONTEXT_LOCATION means the snapshot lists active sessions at a location (the only context implemented today). |
| PRESENCE_CONTEXT_SCENE | 2 | PRESENCE_CONTEXT_SCENE is wire-reserved for scene-focus presence; the resolver lands in a follow-up bead and the RPC currently returns UNIMPLEMENTED for scene-focused sessions. |



<a name="holomush-core-v1-PresenceState"></a>

### PresenceState
PresenceState describes a character&#39;s presence status within a focus context.

| Name | Number | Description |
| ---- | ------ | ----------- |
| PRESENCE_STATE_UNSPECIFIED | 0 | PRESENCE_STATE_UNSPECIFIED is the zero value; not emitted on success. |
| PRESENCE_STATE_ACTIVE | 1 | PRESENCE_STATE_ACTIVE means the character has an active session in the context. This is the only state the location resolver emits today. |
| PRESENCE_STATE_DETACHED | 2 | PRESENCE_STATE_DETACHED is reserved for the future scene resolver (character present in the scene but with a detached transport). |
| PRESENCE_STATE_INACTIVE | 3 | PRESENCE_STATE_INACTIVE is reserved for the future scene resolver. |


 

 


<a name="holomush-core-v1-CoreService"></a>

### CoreService
CoreService is the game-core gRPC surface served by internal/grpc.CoreServer
(registered in cmd/holomush/sub_grpc.go via RegisterCoreServiceServer). It is
the single entry point through which gateways (telnet, web) drive gameplay,
authentication, session management, and event streaming. The gateway is a
pure protocol translator (see .claude/rules/gateway-boundary.md); all game
state and business logic lives behind these RPCs.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| HandleCommand | [HandleCommandRequest](#holomush-core-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-core-v1-HandleCommandResponse) | HandleCommand validates session ownership, records the command in session history, and dispatches it through the unified command dispatcher. Handler output is NOT returned inline — it is emitted as command_response events on the character&#39;s own stream. The RPC reply carries only success/failure. Auth: requires a player_session_token matching session_id; ownership failures collapse to a generic &#34;session not found&#34; in the response body. |
| Subscribe | [SubscribeRequest](#holomush-core-v1-SubscribeRequest) | [SubscribeResponse](#holomush-core-v1-SubscribeResponse) stream | Subscribe opens the long-lived server-streaming event feed for a session. The server (not the client) determines which streams to deliver and the replay policy via FocusCoordinator.RestoreFocus; it registers the caller&#39;s connection, replays history, then forwards live events. Ownership is validated up front and collapses to SESSION_NOT_FOUND on any failure. |
| Disconnect | [DisconnectRequest](#holomush-core-v1-DisconnectRequest) | [DisconnectResponse](#holomush-core-v1-DisconnectResponse) | Disconnect detaches a connection (or the whole session) and is idempotent: an already-gone session returns success. It validates ownership first, then removes the named connection and tears down session state. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-core-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-core-v1-GetCommandHistoryResponse) | GetCommandHistory returns the recent commands recorded for a session (the per-session ring buffer maintained by sessionStore.AppendCommand). Ownership is validated; this is distinct from event history (QueryStreamHistory). |
| AuthenticatePlayer | [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest) | [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse) | AuthenticatePlayer is phase one of two-phase login: it verifies username and password, enforces the per-player session cap, mints a PlayerSession, and returns the bearer token plus the player&#39;s character roster. No game session exists yet — that requires a follow-up SelectCharacter call. |
| SelectCharacter | [SelectCharacterRequest](#holomush-core-v1-SelectCharacterRequest) | [SelectCharacterResponse](#holomush-core-v1-SelectCharacterResponse) | SelectCharacter is phase two of two-phase login: given a valid player session token, it reattaches an existing detached game session (preserving scrollback) or creates a fresh one for the chosen character, emitting an arrive event. The character must belong to the authenticated player. |
| CreatePlayer | [CreatePlayerRequest](#holomush-core-v1-CreatePlayerRequest) | [CreatePlayerResponse](#holomush-core-v1-CreatePlayerResponse) | CreatePlayer registers a new player account and immediately returns a player session token (the new account is logged in). The returned character roster is empty — a freshly created player has no characters until CreateCharacter. |
| CreateGuest | [CreateGuestRequest](#holomush-core-v1-CreateGuestRequest) | [CreateGuestResponse](#holomush-core-v1-CreateGuestResponse) | CreateGuest provisions an ephemeral guest player plus one starter character and returns a short-lived (guest TTL) player session token. Used by the &#34;play as guest&#34; entry path; no credentials are required. |
| CreateCharacter | [CreateCharacterRequest](#holomush-core-v1-CreateCharacterRequest) | [CreateCharacterResponse](#holomush-core-v1-CreateCharacterResponse) | CreateCharacter adds a character to the authenticated player&#39;s roster. When a transactor and bindings service are configured, the character row and its ownership binding are created atomically in one transaction. |
| ListCharacters | [ListCharactersRequest](#holomush-core-v1-ListCharactersRequest) | [ListCharactersResponse](#holomush-core-v1-ListCharactersResponse) | ListCharacters returns the authenticated player&#39;s character roster enriched with per-character session status and last-known location. |
| RequestPasswordReset | [RequestPasswordResetRequest](#holomush-core-v1-RequestPasswordResetRequest) | [RequestPasswordResetResponse](#holomush-core-v1-RequestPasswordResetResponse) | RequestPasswordReset begins a password-reset flow for the given email. The reply is ALWAYS success regardless of whether the email exists — this is an intentional enumeration-prevention measure; delivery is stubbed (logged). |
| ConfirmPasswordReset | [ConfirmPasswordResetRequest](#holomush-core-v1-ConfirmPasswordResetRequest) | [ConfirmPasswordResetResponse](#holomush-core-v1-ConfirmPasswordResetResponse) | ConfirmPasswordReset completes the flow by validating the reset token and setting the new password. Failure messages are sanitized so token/internal detail does not leak to the client. |
| Logout | [LogoutRequest](#holomush-core-v1-LogoutRequest) | [LogoutResponse](#holomush-core-v1-LogoutResponse) | Logout deletes the caller&#39;s PlayerSession and, before doing so, fans out disconnect &#43; session_ended &#43; delete &#43; hooks to every child game session so no Subscribe stream is left orphaned. Per-session signals complete before the PlayerSession row is deleted to avoid ownership-validation flapping. |
| CheckPlayerSession | [CheckPlayerSessionRequest](#holomush-core-v1-CheckPlayerSessionRequest) | [CheckPlayerSessionResponse](#holomush-core-v1-CheckPlayerSessionResponse) | CheckPlayerSession validates a player session token and returns the player identity plus character roster. Used by the web gateway for cookie-based auth checks. The failure path returns an Unauthenticated status (not a body flag), preserving the enumeration-safety contract for unknown/expired sessions. |
| ListPlayerSessions | [ListPlayerSessionsRequest](#holomush-core-v1-ListPlayerSessionsRequest) | [ListPlayerSessionsResponse](#holomush-core-v1-ListPlayerSessionsResponse) | ListPlayerSessions returns the caller&#39;s active PlayerSessions (the rows in player_sessions for the caller&#39;s player_id). Tokens are never returned — only metadata useful for user-visible session management (&#34;you are signed in on these devices&#34;). Any auth failure returns an empty list, so callers cannot distinguish an invalid token from a player with zero sessions. |
| RevokePlayerSession | [RevokePlayerSessionRequest](#holomush-core-v1-RevokePlayerSessionRequest) | [RevokePlayerSessionResponse](#holomush-core-v1-RevokePlayerSessionResponse) | RevokePlayerSession deletes one specific PlayerSession. Ownership is verified: a player cannot revoke another player&#39;s session, and cross-player attempts collapse to &#34;session not found&#34; (logged WARN for security audit). |
| RevokeOtherPlayerSessions | [RevokeOtherPlayerSessionsRequest](#holomush-core-v1-RevokeOtherPlayerSessionsRequest) | [RevokeOtherPlayerSessionsResponse](#holomush-core-v1-RevokeOtherPlayerSessionsResponse) | RevokeOtherPlayerSessions deletes all of the caller&#39;s PlayerSessions except the current one. Convenience bulk operation equivalent to listing and calling RevokePlayerSession for each — useful after a suspected compromise. |
| QueryStreamHistory | [QueryStreamHistoryRequest](#holomush-core-v1-QueryStreamHistoryRequest) | [QueryStreamHistoryResponse](#holomush-core-v1-QueryStreamHistoryResponse) | QueryStreamHistory reads paginated event history from a single stream. It is a pure read that does NOT mutate session cursors (invariant I-13). Two-layer authorization applies: private streams (character / scene) use a hard membership gate (I-17, no ABAC, no admin override); public streams (location, global) are evaluated by the ABAC engine. History transparently spans the recent JetStream tier and the older PostgreSQL audit tier. |
| ListSessionStreams | [ListSessionStreamsRequest](#holomush-core-v1-ListSessionStreamsRequest) | [ListSessionStreamsResponse](#holomush-core-v1-ListSessionStreamsResponse) | ListSessionStreams returns the stream names the session is currently subscribed to, derived from FocusCoordinator.RestoreFocus (with the same ambient-stream fallback Subscribe uses). Web clients use it to enumerate streams for backfill on reload. Pure read; ownership-validated and enumeration-safe (failures collapse to SESSION_NOT_FOUND), closing the IDOR where one player could enumerate another&#39;s subscribed streams. |
| ListFocusPresence | [ListFocusPresenceRequest](#holomush-core-v1-ListFocusPresenceRequest) | [ListFocusPresenceResponse](#holomush-core-v1-ListFocusPresenceResponse) | ListFocusPresence returns the current-state presence snapshot for the session&#39;s focus context. It reads session.Store.ListActiveByLocation directly (NOT event history — see .claude/rules/event-interfaces.md) and is gated by the ABAC list_presence action on the location resource. Scene-focus contexts currently return UNIMPLEMENTED. Pure read — no session mutation. |
| ListAvailableCommands | [ListAvailableCommandsRequest](#holomush-core-v1-ListAvailableCommandsRequest) | [ListAvailableCommandsResponse](#holomush-core-v1-ListAvailableCommandsResponse) | ListAvailableCommands returns the commands the session&#39;s own character may execute, with the system/manifest alias map for those commands. SERVED: CoreServer.ListAvailableCommands, delegating to commandquery.Querier.Available. Self-scoped: the subject is the session&#39;s character (ownership-validated), never an arbitrary character_id. Pure read. |
| RefreshConnection | [RefreshConnectionRequest](#holomush-core-v1-RefreshConnectionRequest) | [RefreshConnectionResponse](#holomush-core-v1-RefreshConnectionResponse) | RefreshConnection bumps a connection&#39;s liveness lease. Called periodically by the gateway while the client socket is open (holomush-rsoe6). SERVED by CoreServer.RefreshConnection; ownership-validated and enumeration-safe. |

 



<a name="holomush_admin_v1_read_stream-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/admin/v1/read_stream.proto



<a name="holomush-admin-v1-AdminReadStreamRequest"></a>

### AdminReadStreamRequest
AdminReadStreamRequest is the operator break-glass read request for the
AdminReadStream RPC. The handler (internal/admin/readstream/handler.go)
validates and canonicalises this into a domestic Request via protoToDomesticRequest
before passing it to ResolveBounds. Fields fall into three categories:
identity (session_token), query shape (subject_pattern, type_filter, context,
since, until, limit), and authorization metadata (dual_control,
dual_control_timeout_seconds, justification). The justification field is
REQUIRED at the application layer: ResolveBounds returns
DENY_OPERATOR_READ_JUSTIFICATION_EMPTY when it is absent or whitespace-only.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token is the bearer token identifying the operator. The handler resolves it to an OperatorSession via SessionStore.GetOperatorSession and then checks that the resolved player holds the crypto.operator ABAC grant (INV-CRYPTO-55) before any data read or audit publish occurs. |
| subject_pattern | [string](#string) |  | subject_pattern is an optional additional NATS subject filter applied server-side on top of the context-derived subjects. An empty string means no additional filter; the handler uses the context-derived subjects alone. |
| type_filter | [string](#string) |  | type_filter is an optional event type prefix filter. When non-empty, only events whose type string has this prefix are returned. An empty string means no type filtering. |
| context | [ContextRef](#holomush-admin-v1-ContextRef) | repeated | context scopes the read to one or more event streams. Each entry maps to a NATS wildcard subject &#34;events.&lt;game&gt;.&lt;type&gt;.&lt;id...&gt;.&gt;&#34; via BuildSubjects (internal/admin/readstream/subjects.go). When empty, a single game-wide wildcard &#34;events.&lt;game&gt;.&gt;&#34; is used. Up to 64 context entries are accepted; ResolveBounds validates type, arity, and ID format per sensitiveTypes. |
| since | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | since is the inclusive lower bound of the query window. When absent (nil), the server defaults to now minus the configured DefaultWindow (INV-CRYPTO-56). ResolveBounds rejects since &gt;= until with DENY_OPERATOR_READ_TIME_INVERTED. |
| until | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | until is the exclusive upper bound of the query window. When absent (nil), the server defaults to now (INV-CRYPTO-56). ResolveBounds rejects until more than 5 seconds in the future with DENY_OPERATOR_READ_FUTURE_BOUND. |
| limit | [uint32](#uint32) |  | limit caps the maximum number of EventFrame responses the client wants to receive. A value of 0 means no client-imposed limit; the server enforces its own window-size ceiling independently via MaxWindow. |
| dual_control | [bool](#bool) |  | dual_control requires a second operator to approve the request before the stream begins. When true, the server sends a PendingApproval frame and blocks until approval.Repo.WaitForApproval resolves or the ApprovalTTL elapses (INV-CRYPTO-61/INV-CRYPTO-67). When false, the fast single-control path runs immediately after the capability check. |
| dual_control_timeout_seconds | [uint32](#uint32) |  | dual_control_timeout_seconds overrides the server&#39;s configured ApprovalTTL for this request. A value of 0 uses the server&#39;s default. |
| justification | [string](#string) |  | justification is the operator&#39;s plain-text reason for the read. REQUIRED: ResolveBounds rejects empty or whitespace-only values with DENY_OPERATOR_READ_JUSTIFICATION_EMPTY. Maximum 4096 UTF-8 bytes. Captured verbatim in the pre-data audit payload (INV-CRYPTO-53/INV-CRYPTO-57). |






<a name="holomush-admin-v1-AdminReadStreamResponse"></a>

### AdminReadStreamResponse
AdminReadStreamResponse is the server-streaming response envelope for the
AdminReadStream RPC. Exactly one payload variant is populated per frame.
The stream follows a fixed lifecycle: an optional PendingApproval frame (only
when dual_control=true), exactly one ReadStarted frame once streaming begins,
zero or more EventFrame frames, and exactly one ReadFinished frame as the
terminal message. The handler (internal/admin/readstream/handler.go
handleInternal) enforces the audit invariants: the pre-data audit is emitted
before the first frame (INV-CRYPTO-53/INV-CRYPTO-54) and the post-data audit is emitted after
the final frame (INV-CRYPTO-60).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| pending_approval | [PendingApproval](#holomush-admin-v1-PendingApproval) |  | pending_approval is sent when dual_control=true and a second operator must approve before streaming begins. Present at most once, before started. |
| started | [ReadStarted](#holomush-admin-v1-ReadStarted) |  | started is sent once the capability check, optional dual-control approval, and pre-data audit publish all succeed. Carries the resolved request parameters so the client can confirm the effective query window. |
| event | [holomush.core.v1.EventFrame](#holomush-core-v1-EventFrame) |  | event is a single event frame from the cold-tier audit log. Each frame carries either the decrypted payload (metadata_only=false) or, when decryption fails, only event metadata (metadata_only=true) with no_plaintext_reason set. Uses corev1.EventFrame for typed redaction (metadata_only &#43; no_plaintext_reason), not eventbusv1.Event (ADR-0017). |
| finished | [ReadFinished](#holomush-admin-v1-ReadFinished) |  | finished is the terminal frame, sent after all events have been delivered or on any error or timeout. Always present as the last frame. |






<a name="holomush-admin-v1-ContextRef"></a>

### ContextRef
ContextRef is a typed, variable-arity scope reference that maps to a NATS
subject wildcard. The type selects the event domain (e.g. &#34;scene&#34;,
&#34;location&#34;, &#34;character&#34;, &#34;dm&#34;) and ids supplies the entity identifiers. The
handler validates type membership and arity against sensitiveTypes
(internal/admin/readstream/filter.go) and rejects unknown types with
DENY_OPERATOR_READ_TYPE_UNKNOWN and wrong-arity entries with
DENY_OPERATOR_READ_ARITY_MISMATCH. For order-insensitive types (e.g. &#34;dm&#34;),
IDs are lex-sorted during canonicalisation so that A→B and B→A are treated
as the same context.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  | type names the event domain being scoped. Recognised values are &#34;scene&#34;, &#34;location&#34;, &#34;character&#34;, and &#34;dm&#34;. ResolveBounds rejects unrecognised types with DENY_OPERATOR_READ_TYPE_UNKNOWN. |
| ids | [string](#string) | repeated | ids are the entity identifiers for this context, each a 26-char Crockford Base32 ULID. The required count (arity) depends on the type: &#34;scene&#34;, &#34;location&#34;, and &#34;character&#34; each require exactly one ID; &#34;dm&#34; requires exactly two (the pair of participant character IDs, lex-sorted by the handler for canonicalisation). |






<a name="holomush-admin-v1-PendingApproval"></a>

### PendingApproval
PendingApproval is sent when dual_control=true and a second operator must
approve before streaming begins. The handler emits this frame after opening a
new approval row in approval.Repo and before calling WaitForApproval
(internal/admin/readstream/handler.go acquireApproval). The client should
display the request_id so the approving operator can locate the pending row.
If WaitForApproval times out before approval, the stream closes with
ReadFinished{TERMINATED_BY_DUAL_CONTROL_TIMEOUT}.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [bytes](#bytes) |  | request_id is the raw 16-byte ULID of the pending approval row. Clients display or log this for the second operator to use when looking up the pending approval. Wire format is raw bytes (not Base32 string). |
| expires_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | expires_at is the wall-clock deadline by which a second operator must approve. Derived from server clock plus the configured ApprovalTTL at the moment the approval row was opened. |






<a name="holomush-admin-v1-ReadFinished"></a>

### ReadFinished
ReadFinished is the terminal frame sent after the last EventFrame (or
immediately when an error, timeout, or disconnect terminates the stream
before any events). Always present as the final message in the stream.
The handler builds this frame in buildFinishedFrame and emits it
best-effort even after send failures (internal/admin/readstream/handler.go).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| terminated_by | [ReadFinished.TerminatedBy](#holomush-admin-v1-ReadFinished-TerminatedBy) |  | terminated_by reports why the stream ended. Mapped from the streamErr by classifyTerminator (internal/admin/readstream/handler.go). CLIENT_EOF indicates a clean completion; all other values indicate some form of interruption or failure. |
| events_scanned | [int64](#int64) |  | events_scanned is the total count of cold-tier audit rows that the handler processed during the stream (including rows that failed decryption and became metadata-only frames). |
| decrypt_fail_count | [int64](#int64) |  | decrypt_fail_count is the count of rows where decryption failed and a metadata-only EventFrame was emitted instead of a plaintext frame. |
| finished_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | finished_at is the server wall-clock time when the ReadFinished frame was built, stamped by handler Config.Clock. |






<a name="holomush-admin-v1-ReadStarted"></a>

### ReadStarted
ReadStarted is sent exactly once when the operator&#39;s read clears all
gates (capability check, optional dual-control approval, and pre-data audit
publish) and the stream is about to deliver EventFrame messages. Carries the
resolved query parameters so the client can confirm the effective window and
contexts. The handler builds this frame in buildStartedFrame
(internal/admin/readstream/handler.go).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | request_id is the 26-character Crockford Base32 ULID for this read operation, generated fresh by the handler via idgen.New(). Stamped in both the pre-data and post-data audit payloads for correlation. |
| policy_hash | [bytes](#bytes) |  | policy_hash is the raw 32-byte SHA-256 of the active site policy at the time the read was authorised. Decoded from the &#34;sha256:&lt;hex&gt;&#34; string held in handler Config.PolicyHash (a required config — an empty hash is rejected at startup). The audit payload stores the canonical &#34;sha256:&lt;hex&gt;&#34; form; this field delivers the raw bytes, and is empty only if that configured string fails to decode (a defensive guard). |
| resolved_since | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | resolved_since is the effective lower bound of the query window after ResolveBounds defaulting. Always populated; equals since from the request when the client supplied a value, otherwise derived as now-DefaultWindow. |
| resolved_until | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | resolved_until is the effective upper bound of the query window after ResolveBounds defaulting. Always populated; equals until from the request when the client supplied a value, otherwise derived as now. |
| resolved_contexts | [ContextRef](#holomush-admin-v1-ContextRef) | repeated | resolved_contexts are the canonicalised context entries after ResolveBounds validation, deduplication, and lex-sorting of order-insensitive IDs (e.g. &#34;dm&#34; participants). May differ from the request context when the client submitted duplicates or unsorted &#34;dm&#34; IDs. |





 


<a name="holomush-admin-v1-ReadFinished-TerminatedBy"></a>

### ReadFinished.TerminatedBy
TerminatedBy enumerates the reason the AdminReadStream stream ended.
Mapped from the internal streamErr by classifyTerminator
(internal/admin/readstream/handler.go). The labels are also written to
the post-data audit payload&#39;s &#34;terminated_by&#34; string field via
terminatedByLabel.

| Name | Number | Description |
| ---- | ------ | ----------- |
| TERMINATED_BY_UNSPECIFIED | 0 | TERMINATED_BY_UNSPECIFIED is the zero/default value; not used in production — classifyTerminator always resolves to a specific variant. |
| TERMINATED_BY_CLIENT_EOF | 1 | TERMINATED_BY_CLIENT_EOF indicates the cold-tier scan finished cleanly with no error (streamErr == nil). All requested events were delivered. |
| TERMINATED_BY_CLIENT_DISCONNECT | 2 | TERMINATED_BY_CLIENT_DISCONNECT indicates the client disconnected mid-stream. Mapped from context.Canceled by classifyTerminator. |
| TERMINATED_BY_DEADLINE_EXCEEDED | 3 | TERMINATED_BY_DEADLINE_EXCEEDED indicates either the request context deadline was exceeded (context.DeadlineExceeded) or a per-frame write deadline fired (ErrWriteDeadlineExceeded, INV-CRYPTO-64) during streaming. |
| TERMINATED_BY_SERVER_ERROR | 4 | TERMINATED_BY_SERVER_ERROR indicates an unexpected server-side failure (cold-reader error, codec failure, or other unclassified error). Mapped by the classifyTerminator catch-all branch. |
| TERMINATED_BY_DUAL_CONTROL_TIMEOUT | 5 | TERMINATED_BY_DUAL_CONTROL_TIMEOUT indicates the ApprovalTTL elapsed before a second operator approved the request (INV-CRYPTO-61/INV-CRYPTO-67). Mapped from READSTREAM_DUAL_CONTROL_TIMEOUT oops code. |
| TERMINATED_BY_AUDIT_EMIT_FAILURE | 6 | TERMINATED_BY_AUDIT_EMIT_FAILURE indicates the pre-data audit publish (EmitStart) failed before any event data was read or sent. Mapped from DENY_AUDIT_PRE_DATA_PUBLISH oops code (INV-CRYPTO-54). No event data was delivered when this value appears. |


 

 

 



<a name="holomush_admin_v1_rekey-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/admin/v1/rekey.proto



<a name="holomush-admin-v1-Phase3Progress"></a>

### Phase3Progress
Phase3Progress reports incremental progress during Phase 3, the bulk
cold-tier re-encryption phase. The orchestrator rewrites events_audit rows
in batches of up to 1000, decrypting each under the old DEK and
re-encrypting under the new DEK with AAD rebound to the new (dek_ref,
dek_version) — INV-CRYPTO-95. Clients may use these messages to render a
progress bar; the stream is terminated by RekeyCompleted or RekeyError.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows_rewritten | [int64](#int64) |  | rows_rewritten is the cumulative count of events_audit rows re-encrypted by this Phase 3 invocation so far. Resets to zero on a fresh resume; the checkpoint row&#39;s phase3_rows_rewritten column holds the cross-resume total. |
| rows_remaining_estimate | [int64](#int64) |  | rows_remaining_estimate is a best-effort count of events_audit rows whose dek_ref still points at the old DEK. Not guaranteed to be exact (rows may be written concurrently); use for display only. |
| last_processed_event_id | [bytes](#bytes) |  | last_processed_event_id is the ULID bytes of the most recently committed batch&#39;s last row. Stored as the Phase 3 resume cursor in the checkpoint row (INV-CRYPTO-94); a crash and resume picks up exactly where this cursor points. |






<a name="holomush-admin-v1-Phase5Attempt"></a>

### Phase5Attempt
Phase5Attempt is emitted each time the orchestrator retries the Phase 5
cluster cache-invalidation fan-out. Phase 5 requests every replica to evict
the old DEK from its in-memory cache; it succeeds only when all members
acknowledge. Timeout surfaces missing_members; the operator may retry
(RekeyResume) or bypass quorum via force_destroy (RekeyResumeRequest).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_count | [int32](#int32) |  | attempt_count is the 1-based index of this invalidation attempt. The checkpoint row&#39;s phase5_attempt_count column is incremented before each attempt and is authoritative; this field mirrors it for live display. |
| missing_members | [string](#string) | repeated | missing_members lists the node identifiers that have not yet acknowledged the cache-invalidation request. Empty on a successful attempt. |






<a name="holomush-admin-v1-PhaseCompleted"></a>

### PhaseCompleted
PhaseCompleted is emitted when an orchestrator phase finishes without error.
Paired with PhaseStarted for bracketing display; the phase string matches
the CheckpointStatus FSM constant of the phase that just finished.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| phase | [string](#string) |  | phase is the FSM status name of the phase that finished successfully, matching the CheckpointStatus constants in checkpoint_fsm.go. |






<a name="holomush-admin-v1-PhaseStarted"></a>

### PhaseStarted
PhaseStarted is emitted at the beginning of each named orchestrator phase.
The phase string matches the CheckpointStatus FSM constants (e.g.
&#34;phase1_auth&#34;, &#34;phase3_reencrypt_cold&#34;) so clients can display a
phase-by-phase progress indicator.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| phase | [string](#string) |  | phase is the FSM status name of the phase that is starting, matching the CheckpointStatus constants in checkpoint_fsm.go (e.g. &#34;phase2_mint_dek&#34;). |






<a name="holomush-admin-v1-RekeyAbortRequest"></a>

### RekeyAbortRequest
RekeyAbortRequest cancels a non-terminal rekey operation, transitioning its
checkpoint to the aborted state. Abort is single-control (INV-CRYPTO-104): any
session holding crypto.operator capability may abort any non-terminal
checkpoint, regardless of site dual-control policy or which operator
initiated the rekey. Once aborted the checkpoint is terminal; a new Rekey
call is required to restart.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token authenticates the aborting operator. Only crypto.operator capability is required — no admin role re-check (INV-CRYPTO-104). |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID of the checkpoint to abort. The handler rejects zero bytes with REKEY_INVALID_REQUEST_ID. If the checkpoint is already terminal (complete or aborted), the handler returns DEK_REKEY_CHECKPOINT_TERMINAL. |






<a name="holomush-admin-v1-RekeyAbortResponse"></a>

### RekeyAbortResponse
RekeyAbortResponse confirms that the rekey checkpoint has been transitioned
to the aborted terminal state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| aborted_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | aborted_at is the server timestamp at which the checkpoint was marked aborted in crypto_rekey_checkpoints. |
| audit_event_id | [bytes](#bytes) |  | audit_event_id is the 16-byte ULID of the abort audit event emitted to events_audit. Operators can use this to correlate the abort with the full rekey operation history via AdminReadStream. |






<a name="holomush-admin-v1-RekeyCompleted"></a>

### RekeyCompleted
RekeyCompleted is the terminal success event emitted at the end of a
successful rekey operation. All 7 phases have completed: a new DEK has been
minted, all cold-tier events_audit rows re-encrypted under it, the old DEK
destroyed, and a chained audit event emitted (Phase 7). The stream ends
after this message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID of the checkpoint row that tracked this rekey operation, matching the value returned by Phase 1 and stored in crypto_rekey_checkpoints.request_id. |
| audit_event_id | [bytes](#bytes) |  | audit_event_id is the 16-byte ULID of the Phase 7 chained rekey audit event emitted to events_audit. Operators can retrieve this event via AdminReadStream for an end-to-end verification trace. |
| duration_ms | [int64](#int64) |  | duration_ms is the wall-clock time in milliseconds from Phase 1 checkpoint open (started_at) to Phase 7 completion (completed_at), measured using the server&#39;s local clock. Used for operational observability. |
| phase3_rows_rewritten | [int64](#int64) |  | phase3_rows_rewritten is the cumulative count of events_audit rows that were re-encrypted during Phase 3 across all resume attempts. This value is read from the checkpoint row&#39;s phase3_rows_rewritten column at completion, which is incremented atomically inside each batch transaction. |
| phase5_attempts | [int32](#int32) |  | phase5_attempts is the total number of cluster cache-invalidation attempts made during Phase 5, including retries due to missing members. A value of 1 means Phase 5 succeeded on the first try. |
| force_destroy_used | [bool](#bool) |  | force_destroy_used is true when the operator passed force_destroy=true on the final RekeyResume call, bypassing Phase 5 quorum by skipping the cluster invalidation step and proceeding directly to Phase 6 (old DEK soft-delete). Recorded for audit traceability. |
| resumed | [bool](#bool) |  | resumed is true when this completion resulted from a RekeyResume call (i.e. the checkpoint was already non-terminal when Run was invoked), as opposed to a fresh Rekey call that drove to completion without interruption. |






<a name="holomush-admin-v1-RekeyError"></a>

### RekeyError
RekeyError is the terminal failure event emitted when the orchestrator
cannot proceed. The stream ends after this message. The checkpoint may
remain non-terminal (e.g. after a Phase 5 timeout), in which case the
operator may call RekeyResume to continue. If the checkpoint has already
transitioned to aborted, RekeyResume will surface DEK_REKEY_CHECKPOINT_TERMINAL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  | code is the oops error code string from the orchestrator, e.g. &#34;DEK_REKEY_PHASE5_TIMEOUT&#34; or &#34;DEK_REKEY_ALREADY_IN_PROGRESS&#34;. Used by operator tooling to branch on specific failure modes. &#34;UNKNOWN&#34; is emitted when the error has no structured code. |
| message | [string](#string) |  | message is the human-readable error description. Not intended for programmatic branching; use code instead. |
| details | [bytes](#bytes) |  | details carries structured context for specific error codes as JSON-encoded bytes, e.g. {&#34;missing_members&#34;:[&#34;node-a&#34;]} for DEK_REKEY_PHASE5_TIMEOUT. Absent (zero-length) when the error code carries no structured detail. |






<a name="holomush-admin-v1-RekeyListRequest"></a>

### RekeyListRequest
RekeyListRequest queries the operator&#39;s view of active and optionally
terminal rekey checkpoints. Results are streamed as RekeyStatusResponse
messages. By default only non-terminal checkpoints are included
(pending through phase7_audit); set include_terminal=true to also receive
complete and aborted rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token authenticates the querying operator. Only crypto.operator capability is required for this read-only RPC. |
| include_terminal | [bool](#bool) |  | include_terminal, when true, includes checkpoints in the complete and aborted terminal states alongside the non-terminal ones. Defaults to false so operators see only in-progress work by default. |
| context_pattern | [string](#string) | optional | context_pattern, when present, filters results to checkpoints whose context_type or context_id contains this substring. Absent means no context filter. |
| since | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | optional | since, when present, restricts results to checkpoints whose started_at is at or after this timestamp. Absent means no lower time bound. |
| limit | [int32](#int32) |  | limit caps the number of rows returned. Values ≤0 or &gt;100 are clamped to 100 by the handler (CheckpointListFilter cap in rekey_handler.go). |






<a name="holomush-admin-v1-RekeyProgress"></a>

### RekeyProgress
RekeyProgress is the streaming event envelope shared by the Rekey and
RekeyResume RPCs. Each message carries exactly one event variant via the
oneof. In the current MVP the server emits a single terminal event
(RekeyCompleted or RekeyError); PhaseStarted, Phase3Progress,
Phase5Attempt, and PhaseCompleted are pre-defined for richer per-phase
streaming in a future enhancement.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| phase_started | [PhaseStarted](#holomush-admin-v1-PhaseStarted) |  | phase_started signals the beginning of a named orchestrator phase. |
| phase3_progress | [Phase3Progress](#holomush-admin-v1-Phase3Progress) |  | phase3_progress reports incremental re-encryption progress during Phase 3 (bulk cold-tier rewrite). |
| phase5_attempt | [Phase5Attempt](#holomush-admin-v1-Phase5Attempt) |  | phase5_attempt reports each cluster cache-invalidation attempt during Phase 5, including which replica members have not yet acknowledged. |
| phase_completed | [PhaseCompleted](#holomush-admin-v1-PhaseCompleted) |  | phase_completed signals that a named orchestrator phase finished successfully. |
| completed | [RekeyCompleted](#holomush-admin-v1-RekeyCompleted) |  | completed is the terminal success event emitted once all 7 phases have finished. Receiving this message means the old DEK has been destroyed and the audit chain updated. |
| error | [RekeyError](#holomush-admin-v1-RekeyError) |  | error is the terminal failure event emitted when the orchestrator cannot proceed. The stream ends after this message; the operator may resume via RekeyResume if the checkpoint is non-terminal. |






<a name="holomush-admin-v1-RekeyRequest"></a>

### RekeyRequest
RekeyRequest initiates a fresh DEK rekey operation for a single encryption
context (context_type &#43; context_id). The caller must hold an authenticated
operator session (session_token from AdminService.Authenticate) and the
crypto.operator in-game capability plus the admin role — both are re-asserted
at dispatch time (INV-CRYPTO-83 defense-in-depth). Justification is recorded on
the checkpoint row for audit; approval_request_id links a pending
admin_approvals row when dual-control is required by site policy.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token authenticates the issuing operator. Must be a non-expired token returned by AdminService.Authenticate; the handler re-validates crypto.operator capability and admin role on every call (INV-CRYPTO-83). |
| context_type | [string](#string) |  | context_type identifies the encryption domain, e.g. &#34;scene&#34;. Together with context_id it resolves the active DEK row (old_dek_id) that the orchestrator&#39;s Phase 1 reads from crypto_keys. |
| context_id | [string](#string) |  | context_id is the entity identifier within context_type, e.g. a scene ULID. The orchestrator uses (context_type, context_id) to locate the active DEK and enforce INV-CRYPTO-92 (at most one non-terminal checkpoint per context at a time). |
| justification | [string](#string) |  | justification is a free-text operator rationale stored on the checkpoint row and included in the Phase 7 chained audit event. Required for accountability; non-empty values are enforced by the handler. |
| approval_request_id | [string](#string) | optional | approval_request_id, when present, links a pending admin_approvals row created by the first operator under dual-control policy. Absent for single-control sites. The orchestrator validates the approval row before advancing past Phase 1. |






<a name="holomush-admin-v1-RekeyResumeRequest"></a>

### RekeyResumeRequest
RekeyResumeRequest resumes a paused or interrupted rekey operation identified
by request_id. The orchestrator determines the resume entry point from the
checkpoint&#39;s current FSM status and drives forward from there. INV-CRYPTO-103:
resuming a complete checkpoint is a no-op that re-emits RekeyCompleted.
Resuming an aborted checkpoint surfaces DEK_REKEY_CHECKPOINT_TERMINAL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token authenticates the resuming operator. The handler re-asserts crypto.operator capability and admin role (INV-CRYPTO-83). |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID identifying the checkpoint to resume. Must be non-zero; zero bytes are rejected with REKEY_INVALID_REQUEST_ID. |
| force_destroy | [bool](#bool) |  | force_destroy, when true, instructs the orchestrator to bypass Phase 5 quorum on this resume attempt. If the checkpoint is stuck in phase5_invalidate with missing_members populated, setting this true skips the cluster invalidation and advances directly to Phase 6 (old DEK soft-delete). Irreversible: the old DEK material is destroyed without full cluster acknowledgement. Recorded in force_destroy_used on RekeyCompleted. |






<a name="holomush-admin-v1-RekeyStatusRequest"></a>

### RekeyStatusRequest
RekeyStatusRequest fetches the current FSM state and associated fields of
a single rekey checkpoint by its request_id. Requires crypto.operator
capability (read-only; no admin role re-check).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token authenticates the querying operator. Only crypto.operator capability is required for this read-only RPC. |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID of the checkpoint to fetch. Returns DEK_REKEY_CHECKPOINT_NOT_FOUND if no row exists for this ID. |






<a name="holomush-admin-v1-RekeyStatusResponse"></a>

### RekeyStatusResponse
RekeyStatusResponse describes the current state of one rekey checkpoint.
Returned by RekeyStatus (unary) and streamed by RekeyList (one message per
matching checkpoint). Fields are populated directly from the
crypto_rekey_checkpoints row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID uniquely identifying this checkpoint, matching the value returned by the Phase 1 open and stored in crypto_rekey_checkpoints.request_id. |
| context_type | [string](#string) |  | context_type is the encryption domain for which this rekey was initiated, e.g. &#34;scene&#34;. Together with context_id it identifies the DEK being rekeyed. |
| context_id | [string](#string) |  | context_id is the entity identifier within context_type, e.g. a scene ULID. |
| status | [string](#string) |  | status is the current FSM state of this checkpoint. Values match the CheckpointStatus constants: &#34;pending&#34;, &#34;phase1_auth&#34;, &#34;phase2_mint_dek&#34;, &#34;phase3_reencrypt_cold&#34;, &#34;phase5_invalidate&#34;, &#34;phase6_destroy_old&#34;, &#34;phase7_audit&#34;, &#34;complete&#34;, or &#34;aborted&#34;. |
| primary_player_id | [string](#string) |  | primary_player_id is the player ID of the operator who initiated the rekey (the first operator under dual-control). Used for accountability and is included in the Phase 7 audit event. |
| started_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | started_at is the server timestamp when the checkpoint row was opened (Phase 1 INSERT). Combined with completed_at it bounds the total rekey wall-clock time. |
| last_heartbeat_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | last_heartbeat_at is the server timestamp of the most recent heartbeat written by Phase 3. The sweep worker uses this to TTL-abort stalled checkpoints (INV-CRYPTO-105/INV-CRYPTO-106). A value far in the past indicates a stalled or crashed orchestrator run. |
| completed_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | completed_at is the server timestamp when the checkpoint reached a terminal state (complete or aborted). Zero if not yet terminal. |
| phase5_attempt_count | [int32](#int32) |  | phase5_attempt_count is the total number of cluster cache-invalidation attempts made during Phase 5 for this checkpoint. Incremented before each attempt; zero means Phase 5 has not started yet. |
| phase5_missing_members | [string](#string) | repeated | phase5_missing_members lists the node identifiers that failed to acknowledge the most recent Phase 5 cache-invalidation request. Non-empty indicates a Phase 5 timeout; the operator may resume or use force_destroy. Empty when Phase 5 has not yet run or succeeded. |
| force_destroy | [bool](#bool) |  | force_destroy records whether force_destroy was set on the last RekeyResume call for this checkpoint, bypassing Phase 5 quorum. |
| old_dek_id | [int64](#int64) | optional | old_dek_id is the primary key of the crypto_keys row being replaced. Absent until Phase 1 resolves the active DEK for the context. |
| new_dek_id | [int64](#int64) | optional | new_dek_id is the primary key of the freshly-minted crypto_keys row created by Phase 2. Absent until Phase 2 completes. |





 

 

 

 



<a name="holomush_admin_v1_admin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/admin/v1/admin.proto



<a name="holomush-admin-v1-ApproveRequest"></a>

### ApproveRequest
ApproveRequest carries the approver&#39;s session token and the ID of the
pending approval row to sign off.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token is the approving operator&#39;s bearer token from Authenticate. Used to resolve the approver&#39;s player identity for the self-approval check (INV-CRYPTO-73) and for capability/role re-assertion (INV-CRYPTO-83). |
| request_id | [bytes](#bytes) |  | request_id is the 16-byte ULID of the admin_approvals row to approve. Must be non-zero; the all-zero sentinel is rejected as an invalid forgery shape even though ulid.Parse accepts it.

16-byte ULID |






<a name="holomush-admin-v1-ApproveResponse"></a>

### ApproveResponse
ApproveResponse is empty; a nil error is the success signal.






<a name="holomush-admin-v1-AuthenticateRequest"></a>

### AuthenticateRequest
AuthenticateRequest carries the operator credentials and TOTP code for
the two-factor authentication step that precedes all other admin operations.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  | username is the in-game operator account name for credential lookup. |
| password | [string](#string) |  | password is the operator account password (plaintext over the UNIX socket; the socket path is a trust boundary and the connection is never exposed to the network). |
| totp_code | [string](#string) |  | totp_code is the current TOTP one-time password from the operator&#39;s authenticator app. The provider rejects expired, reused, and locked codes. |






<a name="holomush-admin-v1-AuthenticateResponse"></a>

### AuthenticateResponse
AuthenticateResponse is returned on successful operator authentication.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token is the opaque short-lived bearer token (10-minute TTL) to supply in session_token fields of subsequent admin RPCs. |
| expires_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | expires_at is the UTC timestamp after which session_token will be rejected with DENY_SESSION_EXPIRED. |
| player_id | [string](#string) |  | player_id is the ULID of the authenticated operator&#39;s player record, included so callers can display or log the operator identity. |






<a name="holomush-admin-v1-ResetTOTPRequest"></a>

### ResetTOTPRequest
ResetTOTPRequest identifies the target player whose TOTP enrollment should
be cleared by an authenticated admin operator.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_token | [string](#string) |  | session_token is the operator&#39;s bearer token from Authenticate. |
| target_player_id | [string](#string) |  | target_player_id is the ULID of the player whose TOTP enrollment will be cleared. Must be a valid non-zero ULID; the handler rejects both malformed strings and the all-zero sentinel. |






<a name="holomush-admin-v1-ResetTOTPResponse"></a>

### ResetTOTPResponse
ResetTOTPResponse reports whether the TOTP enrollment was actually present.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cleared | [bool](#bool) |  | cleared is true when the player was TOTP-enrolled and the enrollment was removed. False when the player had no active TOTP enrollment (no-op); mirrors ClearResult.WasEnrolled from internal/admin/auth/reset_handler.go. |






<a name="holomush-admin-v1-StatusRequest"></a>

### StatusRequest
StatusRequest carries no fields; the Status RPC requires no input.






<a name="holomush-admin-v1-StatusResponse"></a>

### StatusResponse
StatusResponse reports the admin socket server&#39;s health and build identity.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| version | [string](#string) |  | version is the server binary version string (set via -X ldflag at build time). |
| healthy | [bool](#bool) |  | healthy is true when the admin socket HTTP server is accepting requests. compositeHandler.Status always returns true; false would only appear if the handler itself were somehow called during shutdown. |





 

 

 


<a name="holomush-admin-v1-AdminService"></a>

### AdminService
AdminService is the break-glass operator administration service. It is
served exclusively over a UNIX domain socket (admin.sock) and is never
exposed over the network. The compositeHandler implementation delegates
each RPC to a registered handler; unregistered RPCs return Unimplemented,
allowing incremental feature deployment without breaking callers.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Status | [StatusRequest](#holomush-admin-v1-StatusRequest) | [StatusResponse](#holomush-admin-v1-StatusResponse) | Status returns the admin-socket server&#39;s liveness state and the binary version string stamped at build time. No authentication is required; it is intended as a health-check endpoint for operators and monitoring. Implemented directly in compositeHandler; never returns an error. |
| Authenticate | [AuthenticateRequest](#holomush-admin-v1-AuthenticateRequest) | [AuthenticateResponse](#holomush-admin-v1-AuthenticateResponse) | Authenticate verifies operator credentials (username &#43; password) and a TOTP one-time code, then issues a short-lived (10-minute) opaque session token. The token is returned in session_token and must be supplied in subsequent Approve, ResetTOTP, and Rekey* RPCs. Requires the caller to hold the crypto.operator capability and the admin role (validated inside OperatorAuthProvider via AssertOperatorAdmin, internal/admin/auth). Returns DENY_INVALID_CREDENTIALS / DENY_BAD_TOTP / DENY_NOT_OPERATOR / DENY_NOT_ADMIN_ROLE on rejection; DENY_LOCKED when TOTP is rate-limited. |
| Approve | [ApproveRequest](#holomush-admin-v1-ApproveRequest) | [ApproveResponse](#holomush-admin-v1-ApproveResponse) | Approve is the second-operator signoff on a pending admin_approvals row. The caller supplies their session_token (proving identity and live operator status) and the request_id of the approval row to sign off. Repo.MarkApproved atomically enforces three invariants: INV-CRYPTO-72 (the row must not be expired), INV-CRYPTO-73 (the approver cannot be the same player as the primary operator who opened the row), and INV-CRYPTO-74 (each row may only be approved once). Requires the crypto.operator capability and admin role re-checked at call time (INV-CRYPTO-83); handler in internal/admin/approval. |
| ResetTOTP | [ResetTOTPRequest](#holomush-admin-v1-ResetTOTPRequest) | [ResetTOTPResponse](#holomush-admin-v1-ResetTOTPResponse) | ResetTOTP clears a target player&#39;s TOTP enrollment, allowing them to re-enroll on next login. On success, AuditingService.ClearTOTP emits a crypto.totp_cleared audit event with cleared_by=&#34;admin_reset&#34; (T13). Response.cleared is false when the player was not enrolled (no-op). Requires a valid session_token with the crypto.operator capability and admin role re-checked at call time (INV-CRYPTO-83); handler in internal/admin/auth (reset_handler.go). |
| Rekey | [RekeyRequest](#holomush-admin-v1-RekeyRequest) | [RekeyProgress](#holomush-admin-v1-RekeyProgress) stream | Rekey initiates a fresh DEK rekey for the given context. Requires the crypto.operator capability and admin role (re-checked at call time, INV-CRYPTO-83). Streams a single terminal RekeyProgress event: RekeyCompleted on success or RekeyError on orchestrator failure. Per-phase progress updates are pre-defined in the proto but not yet emitted (follow-up). Uses the shared RekeyProgress stream type (also used by RekeyResume) — the buf RPC_REQUEST_RESPONSE_UNIQUE / RPC_RESPONSE_STANDARD_NAME exemptions are intentional (jxo8.7.27). Handler: internal/admin/socket/rekey_handler.go. |
| RekeyResume | [RekeyResumeRequest](#holomush-admin-v1-RekeyResumeRequest) | [RekeyProgress](#holomush-admin-v1-RekeyProgress) stream | RekeyResume resumes a paused or interrupted rekey identified by request_id. Requires the crypto.operator capability and admin role (INV-CRYPTO-83). Idempotency (INV-CRYPTO-103) and same-args invariant (INV-CRYPTO-91) are enforced inside the orchestrator, not here. The handler validates that request_id is a non-zero 16-byte ULID and forwards it to the orchestrator adapter, which looks up the checkpoint to resolve ContextType/ContextID. Streams a terminal RekeyProgress event — same shared type as Rekey. Handler: internal/admin/socket/rekey_handler.go. |
| RekeyAbort | [RekeyAbortRequest](#holomush-admin-v1-RekeyAbortRequest) | [RekeyAbortResponse](#holomush-admin-v1-RekeyAbortResponse) | RekeyAbort cancels an in-progress rekey checkpoint. Requires the crypto.operator capability only; no admin role re-check and no dual-control approval — abort is single-control regardless of site policy (INV-CRYPTO-104). Any crypto.operator session may abort any non-terminal checkpoint, not just the primary operator who started it. Handler: internal/admin/socket/rekey_handler.go. |
| RekeyStatus | [RekeyStatusRequest](#holomush-admin-v1-RekeyStatusRequest) | [RekeyStatusResponse](#holomush-admin-v1-RekeyStatusResponse) | RekeyStatus returns the current state of a single rekey operation identified by request_id. Requires the crypto.operator capability; no admin role re-check. Reads from the crypto_rekey_checkpoints table via CheckpointStatusReader.GetCheckpoint. Handler: internal/admin/socket/rekey_handler.go. |
| RekeyList | [RekeyListRequest](#holomush-admin-v1-RekeyListRequest) | [RekeyStatusResponse](#holomush-admin-v1-RekeyStatusResponse) stream | RekeyList streams status records for rekey operations. By default only non-terminal checkpoints are returned; set include_terminal to include completed and aborted rows. Results are capped at 100 rows (any limit above 100 or zero is silently clamped to 100). Requires the crypto.operator capability; no admin role re-check. Handler: internal/admin/socket/rekey_handler.go. |
| AdminReadStream | [AdminReadStreamRequest](#holomush-admin-v1-AdminReadStreamRequest) | [AdminReadStreamResponse](#holomush-admin-v1-AdminReadStreamResponse) stream | AdminReadStream is the operator break-glass streaming read RPC. Streams EventFrame payloads for the requested context(s) and time bounds, with typed metadata_only and no_plaintext_reason redaction fields for destroyed-DEK and plaintext-suppressed events. When dual_control is set in the request, the handler blocks until a second operator approves via the admin_approvals table before emitting any event frames (INV-CRYPTO-61/INV-CRYPTO-67). Handler: internal/admin/socket/handlers.go (delegated to ReadStreamRPCHandler). |

 



<a name="holomush_content_v1_content-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/content/v1/content.proto



<a name="holomush-content-v1-ContentItem"></a>

### ContentItem
ContentItem is a single managed content record retrieved from the store.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | key is the storage key identifying this item; callers conventionally use dot-delimited names such as &#34;landing.hero&#34;, though the store enforces none. |
| content_type | [string](#string) |  | content_type is the IANA media type of the body, for example &#34;text/markdown&#34; or &#34;application/json&#34;. |
| body | [bytes](#bytes) |  | body is the raw content bytes; interpret according to content_type. |
| metadata | [ContentItem.MetadataEntry](#holomush-content-v1-ContentItem-MetadataEntry) | repeated | metadata holds arbitrary string key/value annotations attached to the item, such as &#34;title&#34;, &#34;icon&#34;, &#34;order&#34;, or &#34;alt&#34;. |






<a name="holomush-content-v1-ContentItem-MetadataEntry"></a>

### ContentItem.MetadataEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-content-v1-GetContentRequest"></a>

### GetContentRequest
GetContentRequest selects a single content item by its exact storage key.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | key is the exact content-store key to retrieve; no prefix matching is performed. |






<a name="holomush-content-v1-GetContentResponse"></a>

### GetContentResponse
GetContentResponse carries the content item for the requested key. A missing
key yields no response message — the RPC fails with a gRPC NotFound status.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| item | [ContentItem](#holomush-content-v1-ContentItem) |  | item is the content item for the requested key. |






<a name="holomush-content-v1-ListContentRequest"></a>

### ListContentRequest
ListContentRequest selects a page of content items whose keys share a common prefix.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| prefix | [string](#string) |  | prefix restricts results to keys that begin with this string; pass an empty string to match all keys. |
| limit | [int32](#int32) |  | limit is the maximum number of items to return per page; zero means no limit. The server does not impose its own cap — callers should set a reasonable bound. |
| cursor | [string](#string) |  | cursor is the next_cursor value from a prior ListContentResponse; pass an empty string to start from the beginning. The value is the key of the last item on the previous page, used for keyset pagination. |






<a name="holomush-content-v1-ListContentResponse"></a>

### ListContentResponse
ListContentResponse carries one page of content items and a pagination token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [ContentItem](#holomush-content-v1-ContentItem) | repeated | items is the slice of content items matching the request prefix, ordered by key. |
| next_cursor | [string](#string) |  | next_cursor is the key of the last returned item; pass it as cursor in a subsequent request to fetch the next page. An empty string means there are no further items. |





 

 

 


<a name="holomush-content-v1-ContentService"></a>

### ContentService
ContentService provides read access to the content store.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetContent | [GetContentRequest](#holomush-content-v1-GetContentRequest) | [GetContentResponse](#holomush-content-v1-GetContentResponse) | GetContent retrieves a single content item by key. |
| ListContent | [ListContentRequest](#holomush-content-v1-ListContentRequest) | [ListContentResponse](#holomush-content-v1-ListContentResponse) | ListContent returns all content items matching a key prefix. |

 



<a name="holomush_control_v1_control-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/control/v1/control.proto



<a name="holomush-control-v1-ShutdownRequest"></a>

### ShutdownRequest
Parameters for a shutdown request. The graceful field is currently logged
but does not alter shutdown behavior — both values invoke the shutdown hook
identically (mismatch tracked in holomush-4gchp).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| graceful | [bool](#bool) |  | When true, the caller intends a drain-and-exit (in-flight requests allowed to complete before the process stops). Currently only logged for observability; the shutdown hook is a parameterless func() and is not yet differentiated on this value. |






<a name="holomush-control-v1-ShutdownResponse"></a>

### ShutdownResponse
Confirmation that the shutdown sequence has been triggered.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | Human-readable confirmation string; currently always &#34;shutdown initiated&#34;. Callers SHOULD NOT parse this value; it exists for operator logs only. |






<a name="holomush-control-v1-StatusRequest"></a>

### StatusRequest
Empty carrier for a status poll. No parameters are required; the server
derives all response fields from its own runtime state.






<a name="holomush-control-v1-StatusResponse"></a>

### StatusResponse
A point-in-time snapshot of the process&#39;s health and identity.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| running | [bool](#bool) |  | True while the process&#39;s internal running flag is set. Set to false only after GracefulStop completes (internal/control/grpc_server.go::Stop). |
| pid | [int32](#int32) |  | Operating-system process ID as returned by os.Getpid(), cast to int32. Safe on all supported platforms; values never exceed int32 range. |
| uptime_seconds | [int64](#int64) |  | Elapsed seconds since the GRPCServer was constructed via NewGRPCServer. Derived from a monotonic time.Time captured at construction. |
| component | [string](#string) |  | Identifies which process component reported this status, e.g. &#34;core&#34; or &#34;gateway&#34;. Set at construction time; never empty (enforced by NewGRPCServer which returns an error for an empty component string). |





 

 

 


<a name="holomush-control-v1-ControlService"></a>

### ControlService
The mTLS-protected admin surface for a running HoloMUSH process.
Both the core server and the gateway register an instance on startup
(see cmd/holomush/deps.go and cmd/holomush/gateway.go). Callers must
present a valid client certificate issued by the game&#39;s root CA.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Shutdown | [ShutdownRequest](#holomush-control-v1-ShutdownRequest) | [ShutdownResponse](#holomush-control-v1-ShutdownResponse) | Triggers an asynchronous process exit via the registered shutdown hook. The RPC returns immediately with a confirmation message; the shutdown callback runs in a background goroutine. Callers should not expect the connection to remain open after the response arrives. Grounded in: internal/control/grpc_server.go::Shutdown |
| Status | [StatusRequest](#holomush-control-v1-StatusRequest) | [StatusResponse](#holomush-control-v1-StatusResponse) | Returns a snapshot of the process&#39;s liveness and identity without requiring authentication beyond the mTLS channel. Reads from an atomic running flag, os.Getpid(), a monotonic start timestamp, and the component label supplied at construction time. Grounded in: internal/control/grpc_server.go::Status |

 



<a name="holomush_eventbus_v1_eventbus-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/eventbus/v1/eventbus.proto



<a name="holomush-eventbus-v1-Actor"></a>

### Actor
Actor identifies who caused an event.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [ActorKind](#holomush-eventbus-v1-ActorKind) |  | kind classifies the entity that caused the event; drives downstream audit routing (e.g. plugin_router.go selects the audit sink by kind). |
| id | [bytes](#bytes) |  | id is the actor&#39;s 16-byte ULID identity, letting downstream audit attribute the event to a concrete entity. Character and plugin actors carry a real ULID; system- and unknown-origin events MAY leave it as the zero ULID, in which case the field is omitted on the wire (see coreActorToEventbusActor). |






<a name="holomush-eventbus-v1-Event"></a>

### Event
Event is the host-side envelope. Wire encoding is proto bytes in the
JetStream message data; headers carry routing/codec/version metadata.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [bytes](#bytes) |  | id is the event&#39;s 16-byte ULID; it is the identity and JetStream dedup key (set as Nats-Msg-Id), stable across rebuilds. Ordering is owned by the JetStream per-stream sequence, not by this ULID&#39;s lexical order. |
| subject | [string](#string) |  | subject is the NATS dot-delimited routing address for this event, of the form events.&lt;game_id&gt;.&lt;domain&gt;.&lt;entity-id&gt;[.&lt;facet&gt;...], validated by NewSubject (must start with &#34;events.&#34;). |
| type | [string](#string) |  | type is the event-type discriminator, e.g. &#34;say&#34; or &#34;scene.pose&#34;; used by subscribers to route and render events without decoding the payload. |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | timestamp records when the event occurred according to the host clock, at nanosecond precision. |
| actor | [Actor](#holomush-eventbus-v1-Actor) |  | actor identifies the entity that caused the event; host-stamped and never directly settable by plugins. |
| payload | [bytes](#bytes) |  | payload is the codec.Encode output for the event body; opaque at the envelope layer and decoded by subscribers according to the codec/version metadata carried in the JetStream message headers. |
| rendering | [holomush.core.v1.RenderingMetadata](#holomush-core-v1-RenderingMetadata) |  | Rendering metadata, populated by RenderingPublisher.Publish before marshaling for JetStream. Mirrors the corev1.RenderingMetadata used on the gRPC Subscribe wire (one schema, two transports). |





 


<a name="holomush-eventbus-v1-ActorKind"></a>

### ActorKind
ActorKind identifies what type of entity caused an event.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACTOR_KIND_UNSPECIFIED | 0 | ACTOR_KIND_UNSPECIFIED is the zero value; a well-formed envelope never carries it — emitters MUST set a concrete kind. |
| ACTOR_KIND_CHARACTER | 1 | ACTOR_KIND_CHARACTER marks an event caused by an in-game character action. |
| ACTOR_KIND_PLAYER | 2 | ACTOR_KIND_PLAYER attributes an event to a human player account rather than a character. It is a recognized wire/audit value preserved across serialization and history round-trips, but no current emit path produces it: host and plugin emits resolve only to CHARACTER, SYSTEM, or PLUGIN (see validateResolvedActor / bridgeActorKind in event_emitter.go). |
| ACTOR_KIND_SYSTEM | 3 | ACTOR_KIND_SYSTEM marks an event the host itself originated (internal infrastructure, not a character or plugin). |
| ACTOR_KIND_PLUGIN | 4 | ACTOR_KIND_PLUGIN marks an event a plugin emitted; gated by the manifest&#39;s actor_kinds_claimable list at event_emitter.go::Emit. |


 

 

 



<a name="holomush_plugin_v1_attribute-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/attribute.proto



<a name="holomush-plugin-v1-AttributeValue"></a>

### AttributeValue
AttributeValue is a discriminated union carrying the runtime value of a
single resolved attribute. Exactly one kind field should be set; the variant
chosen SHOULD match the AttributeType declared in GetSchemaResponse for the
same attribute name.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| string_value | [string](#string) |  | String value for ATTRIBUTE_TYPE_STRING attributes, e.g. owner ULID, state name, or visibility label. |
| number_value | [double](#double) |  | Floating-point value for ATTRIBUTE_TYPE_FLOAT attributes. Stored as double (float64) to cover the full range needed by numeric policy comparisons. |
| bool_value | [bool](#bool) |  | Boolean value for ATTRIBUTE_TYPE_BOOL attributes, e.g. witness flags such as has_location. |
| string_list_value | [StringList](#holomush-plugin-v1-StringList) |  | String-list value for ATTRIBUTE_TYPE_STRING_LIST attributes, e.g. participant character IDs or invitee lists. Use StringList rather than repeated fields to fit within the oneof constraint. |






<a name="holomush-plugin-v1-GetSchemaRequest"></a>

### GetSchemaRequest
GetSchemaRequest carries no parameters. The plugin responds with the full
schema for all resource types it owns; there is no per-type filtering at the
protocol level.






<a name="holomush-plugin-v1-GetSchemaResponse"></a>

### GetSchemaResponse
GetSchemaResponse describes the attribute schema for every resource type the
plugin owns. The host validates that every resource type declared in the
manifest&#39;s resource_types list appears as a key here; a missing key causes
plugin load to fail. The schema is used by the host to build a
types.NamespaceSchema per resource type for the policy engine&#39;s attribute
registry.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| resource_types | [GetSchemaResponse.ResourceTypesEntry](#holomush-plugin-v1-GetSchemaResponse-ResourceTypesEntry) | repeated | Attribute schema keyed by resource type name matching the manifest&#39;s resource_types list (e.g., &#34;scene&#34;, &#34;channel&#34;). Each value describes the attributes resolvable for that type. |






<a name="holomush-plugin-v1-GetSchemaResponse-ResourceTypesEntry"></a>

### GetSchemaResponse.ResourceTypesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [ResourceTypeSchema](#holomush-plugin-v1-ResourceTypeSchema) |  |  |






<a name="holomush-plugin-v1-ResolveResourceRequest"></a>

### ResolveResourceRequest
ResolveResourceRequest identifies the single resource whose attributes the
host needs for an in-flight ABAC policy evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| resource_type | [string](#string) |  | The resource type to resolve, matching one of the keys in the plugin&#39;s GetSchemaResponse. Must be a non-empty string; buf validate enforces min_len = 1. Plugins MUST reject types they do not own with INVALID_ARGUMENT. |
| resource_id | [string](#string) |  | The resource instance identifier, stripped of the &#34;type:&#34; prefix. Must be non-empty; buf validate enforces min_len = 1. For the scene plugin this is the raw scene ULID (e.g., &#34;01JXYZ...&#34;). Plugins SHOULD return NOT_FOUND when this ID is unknown. |






<a name="holomush-plugin-v1-ResolveResourceResponse"></a>

### ResolveResourceResponse
ResolveResourceResponse carries the resolved attribute bag for the requested
resource instance. The host converts this into a map[string]any via
internal/plugin/attribute_proxy.go::convertProtoAttributes and passes it to
the policy engine&#39;s evaluator.

Optional attributes MUST be omitted from the map entirely when unresolved —
do not include a key with an empty-string or zero value as a sentinel. The
DSL evaluator treats absent keys as fail-safe false; a present empty-string
value is NOT the same as absent and may match an unintended policy condition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attributes | [ResolveResourceResponse.AttributesEntry](#holomush-plugin-v1-ResolveResourceResponse-AttributesEntry) | repeated | Resolved attribute values keyed by attribute name, matching the names declared in GetSchemaResponse for the requested resource type. Attributes whose values are unknown or not applicable for this resource instance MUST be omitted from the map rather than included with a zero or empty-string sentinel. |






<a name="holomush-plugin-v1-ResolveResourceResponse-AttributesEntry"></a>

### ResolveResourceResponse.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [AttributeValue](#holomush-plugin-v1-AttributeValue) |  |  |






<a name="holomush-plugin-v1-ResourceTypeSchema"></a>

### ResourceTypeSchema
ResourceTypeSchema describes all resolvable attributes for one resource
type. Every attribute that ResolveResource may ever return for this type
MUST appear here; attributes returned at resolution time but absent from
the schema are ignored by the host&#39;s policy engine.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attributes | [ResourceTypeSchema.AttributesEntry](#holomush-plugin-v1-ResourceTypeSchema-AttributesEntry) | repeated | Attribute names for this resource type, each mapped to its declared type. Names are dot-free strings (e.g., &#34;owner&#34;, &#34;state&#34;, &#34;visibility&#34;). The set MUST be a superset of all keys that ResolveResource can return for this resource type — schema and response MUST be consistent. |






<a name="holomush-plugin-v1-ResourceTypeSchema-AttributesEntry"></a>

### ResourceTypeSchema.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [AttributeType](#holomush-plugin-v1-AttributeType) |  |  |






<a name="holomush-plugin-v1-StringList"></a>

### StringList
StringList wraps a repeated string so it can appear inside the
AttributeValue.kind oneof. Proto3 does not allow repeated fields directly
inside a oneof; this wrapper bridges that constraint.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| values | [string](#string) | repeated | The string elements of the list. Order is preserved; the policy engine treats the list as an ordered sequence for membership checks. |





 


<a name="holomush-plugin-v1-AttributeType"></a>

### AttributeType
AttributeType declares the type of a single attribute in ResourceTypeSchema.
The host uses this declaration to build the policy engine&#39;s type registry,
which drives DSL type-checking and comparison semantics. Mismatches between
declared and returned types produce DSL evaluation errors at runtime.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ATTRIBUTE_TYPE_UNSPECIFIED | 0 | Zero value sentinel. Attributes with UNSPECIFIED type are mapped to AttrTypeString by the host&#39;s schema converter (internal/plugin/attribute_proxy.go::protoAttrTypeToAttrType). Prefer an explicit value in new schemas. |
| ATTRIBUTE_TYPE_STRING | 1 | String-typed attribute. Returned as AttributeValue.string_value in ResolveResourceResponse. Maps to types.AttrTypeString in the host&#39;s policy engine. Used for identifier-like fields such as owner ULID, state enum names, and visibility labels. |
| ATTRIBUTE_TYPE_BOOL | 2 | Boolean-typed attribute. Returned as AttributeValue.bool_value. Maps to types.AttrTypeBool. Useful for binary state flags and witness attributes (has_location, is_archived). |
| ATTRIBUTE_TYPE_FLOAT | 3 | 64-bit floating-point numeric attribute. Returned as AttributeValue.number_value (double). Maps to types.AttrTypeFloat. Used for numeric comparisons in policies, e.g. vote tallies or capacity counts. |
| ATTRIBUTE_TYPE_STRING_LIST | 4 | Ordered list of strings. Returned as AttributeValue.string_list_value. Maps to types.AttrTypeStringList in the policy engine. Used for multi-value fields such as participant character IDs or tag sets. |


 

 


<a name="holomush-plugin-v1-AttributeResolverService"></a>

### AttributeResolverService
AttributeResolverService lets binary plugins expose ABAC attribute resolution
to the host&#39;s policy engine for resource types the plugin owns. The host
auto-registers this service name and calls each plugin that declares
resource_types in its manifest. Plugins MUST NOT list
holomush.plugin.v1.AttributeResolverService in their manifest `provides:`
field — doing so causes SERVICE_ALREADY_REGISTERED at startup. Declare
resource_types in the manifest instead; the host wires the gRPC client
automatically during plugin load (see internal/plugin/manager.go::discoverAndRegisterAttributes).

The interaction has two phases:
 1. Load-time schema discovery: the host calls GetSchema once after Init
    returns and registers a PluginAttributeProvider per declared resource
    type. If the schema does not cover every declared resource type, plugin
    load is rolled back (internal/plugin/manager.go::discoverAndRegisterAttributes).
 2. Per-request attribute resolution: the host calls ResolveResource during
    ABAC policy evaluation whenever a policy references an attribute for one
    of the plugin&#39;s owned resource types. The call is made via the host&#39;s
    PluginAttributeProvider proxy (internal/plugin/attribute_proxy.go::ResolveResource).

Reference implementation: plugins/core-scenes/resolver.go::SceneResolver.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetSchema | [GetSchemaRequest](#holomush-plugin-v1-GetSchemaRequest) | [GetSchemaResponse](#holomush-plugin-v1-GetSchemaResponse) | GetSchema returns the full attribute schema for every resource type this plugin owns. The host calls this exactly once per plugin load, after Init returns, and caches the result for the lifetime of the plugin process. The response MUST include an entry for every resource type declared in the manifest&#39;s resource_types list — missing entries cause load to fail with a hard error and trigger plugin unload rollback. The schema MUST be deterministic across calls; the host does not re-query after the initial load.

See: internal/plugin/manager.go::discoverAndRegisterAttributes (caller), plugins/core-scenes/resolver.go::GetSchema (reference implementation). |
| ResolveResource | [ResolveResourceRequest](#holomush-plugin-v1-ResolveResourceRequest) | [ResolveResourceResponse](#holomush-plugin-v1-ResolveResourceResponse) | ResolveResource returns the current attribute values for a single resource instance identified by type and ID. The host calls this during ABAC policy evaluation when the active policy references an attribute belonging to one of the plugin&#39;s owned resource types. It is invoked per authorization check, not cached. The plugin MUST reject resource_type values it does not own with INVALID_ARGUMENT so host-side misrouting is visible immediately. The plugin SHOULD return NOT_FOUND when the resource ID is unknown.

Optional attributes MUST be omitted from the response map rather than emitted with an empty-string or zero sentinel value. The DSL evaluator treats missing map keys as fail-safe false for every operator; an empty-string value would match any other unresolved empty-string peer and create a fail-open condition. See .claude/rules/abac-providers.md for the full contract.

See: internal/plugin/attribute_proxy.go::ResolveResource (caller), plugins/core-scenes/resolver.go::ResolveResource (reference implementation). |

 



<a name="holomush_plugin_v1_audit-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/audit.proto



<a name="holomush-plugin-v1-AuditEventRequest"></a>

### AuditEventRequest
AuditEventRequest carries a single audit row forwarded by the host
per-plugin consumer for the plugin to persist. The row is built from
the JetStream message by buildAuditRow, which reads projection fields
from the unmarshaled envelope and crypto metadata from NATS headers.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| row | [AuditRow](#holomush-plugin-v1-AuditRow) |  | row is the audit row to persist. MUST be non-nil and MUST pass field validation (non-empty codec, non-nil timestamp, 16-byte id, non-empty type and subject) or the plugin returns an error and the host relies on JetStream redelivery. |






<a name="holomush-plugin-v1-AuditEventResponse"></a>

### AuditEventResponse
AuditEventResponse is the empty acknowledgement returned by the
plugin after a successful idempotent INSERT. The host acks the
JetStream message on receipt.






<a name="holomush-plugin-v1-AuditRow"></a>

### AuditRow
AuditRow is the canonical wire shape for plugin-owned audit rows.
Used in both directions: dispatcher → plugin (AuditEventRequest)
and plugin → host (QueryHistoryResponse). Mirrors the events_audit
row shape so the proto wire format and the storage shape are
coupled.

Cleartext projection fields


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [bytes](#bytes) |  | id holds the 16-byte binary ULID that uniquely identifies this event. Set from the Nats-Msg-Id header. Used as the primary key for idempotent INSERT (ON CONFLICT (id) DO NOTHING). MUST be exactly 16 bytes; the plugin rejects rows with wrong length. |
| subject | [string](#string) |  | subject is the NATS dot-delimited event subject, e.g. &#34;events.&lt;game_id&gt;.scene.&lt;scene_id&gt;.ic&#34;. Used by the plugin to route scene_pose events and as the WHERE clause in queryLog. |
| type | [string](#string) |  | type is the application-level event type string extracted from the App-Event-Type header, e.g. &#34;scene_pose&#34; or &#34;scene_join&#34;. The plugin dispatches on this value to route scene_pose rows through the transactional InsertScenePose path. |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | timestamp is the event wall-clock time stamped at publish. Stored as nanosecond-precision TIMESTAMPTZ in scene_log. MUST be non-nil; the plugin rejects nil timestamps at ingest to prevent SQL NULL from corrupting subsequent queryLog scans. |
| actor | [holomush.eventbus.v1.Actor](#holomush-eventbus-v1-Actor) |  | actor identifies the entity that caused the event. Nil when the event was system-originated (no actor header). Kind is stored as the enum&#39;s String() representation (e.g. &#34;ACTOR_KIND_CHARACTER&#34;). |
| codec | [string](#string) |  | codec names the encryption codec applied to payload. &#34;identity&#34; means payload is plaintext; &#34;xchacha20poly1305-v1&#34; means payload is ciphertext. Sourced from the App-Codec header. MUST be non-empty. |
| payload | [bytes](#bytes) |  | payload holds the event body. For identity codec this is cleartext; for xchacha20poly1305-v1 this is the AEAD ciphertext, forwarded byte-equal without decryption (INV-CRYPTO-46). Plugins store the bytes opaquely; decryption occurs at read-back via DecryptOwnAuditRows. |
| dek_ref | [uint64](#uint64) | optional | dek_ref is the numeric key reference into the host&#39;s crypto_keys table identifying which DEK encrypted this payload. Absent for identity-codec rows; MUST be present for AEAD-codec rows. The host enforces the agreement: identity codec ⇔ both dek_ref and dek_version absent. |
| dek_version | [uint32](#uint32) | optional | dek_version is the 1-based rotation counter of the DEK at the time of encryption, stored for key-rotation audit. Absent for identity-codec rows; MUST be present for AEAD-codec rows alongside dek_ref (INV-EVENTBUS-25). |
| schema_ver | [int32](#int32) |  | schema_ver is the application schema version stamped at publish via the App-Schema-Version header. Valid range 0–32767 (SMALLINT). The plugin rejects rows outside this range at ingest. |






<a name="holomush-plugin-v1-DecryptOwnAuditRowsRequest"></a>

### DecryptOwnAuditRowsRequest
DecryptOwnAuditRowsRequest carries the calling plugin&#39;s OWN audit rows for
host-side read-back decryption (PluginHostService.DecryptOwnAuditRows).
The host enforces OwnerMap subject ownership (g1) per row; rows whose
subject is owned by a different plugin are refused with not_owner and never
decrypted. The batch is REJECTED (not clamped) when it exceeds the
server-side cap of 500.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [AuditRow](#holomush-plugin-v1-AuditRow) | repeated | rows is the batch of audit rows to decrypt. Each row MUST have been previously stored by this plugin (subject ownership enforced by the host&#39;s OwnerMap g1 gate). A batch exceeding 500 rows is rejected outright rather than partially processed. |






<a name="holomush-plugin-v1-DecryptOwnAuditRowsResponse"></a>

### DecryptOwnAuditRowsResponse
DecryptOwnAuditRowsResponse returns one RowResult per request row, in the
same order (1:1 positional correspondence, INV-CRYPTO-37).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| results | [RowResult](#holomush-plugin-v1-RowResult) | repeated | results contains one outcome per request row, in the same order as DecryptOwnAuditRowsRequest.rows. Positional correspondence is guaranteed (INV-CRYPTO-37); callers correlate by index or by RowResult.id. |






<a name="holomush-plugin-v1-QueryHistoryRequest"></a>

### QueryHistoryRequest
QueryHistoryRequest specifies the page of audit rows to stream back
from the plugin&#39;s own audit store. The host&#39;s PluginHistoryRouter
populates this from the eventbus.HistoryQuery and the authenticated
session record.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| subject | [string](#string) |  | subject is the fully-qualified NATS dot-delimited event subject to query, e.g. &#34;events.main.scene.&lt;scene_id&gt;.ic&#34;. MUST be non-empty and MUST NOT contain wildcard tokens (* or &gt;). The plugin parses this to extract the entity identifier for membership checks. |
| after | [bytes](#bytes) |  | after is an exclusive lower-bound cursor encoded as a 16-byte ULID. Rows with id &gt; after are returned. Empty means start from the beginning of the log. ULIDs are time-ordered, so this is equivalent to a chronological lower bound within the subject. |
| before | [bytes](#bytes) |  | before is an exclusive upper-bound cursor encoded as a 16-byte ULID. Rows with id &lt; before are returned. Empty means no upper bound. |
| page_size | [int32](#int32) |  | page_size caps the number of rows returned in this response stream. The host clamps to 200; the plugin MUST also cap at 200 and apply a default of 50 when the value is &lt;= 0. |
| direction | [int32](#int32) |  | direction controls row ordering: 1 = forward (ascending by id, oldest first), 2 = backward (descending by id, newest first). Zero is treated as forward by the plugin. |
| not_before | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | not_before filters out rows whose timestamp is strictly before this value. Applied as a SQL &#34;timestamp &gt;= not_before&#34; predicate. Nil means no lower time bound. |
| not_after | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | not_after filters out rows whose timestamp is strictly after this value. Applied as a SQL &#34;timestamp &lt;= not_after&#34; predicate. Nil means no upper time bound. |
| caller | [holomush.eventbus.v1.Actor](#holomush-eventbus-v1-Actor) |  | caller identifies the principal on whose behalf the host is reading. Plugins implementing PluginAuditService MUST enforce domain-specific authz (e.g., membership) against this identity before returning rows. An absent caller, a zero identity, or an unsupported Actor.Kind MUST be rejected with gRPC PERMISSION_DENIED. The host populates this field from the authenticated session record; clients never supply it. |






<a name="holomush-plugin-v1-QueryHistoryResponse"></a>

### QueryHistoryResponse
QueryHistoryResponse wraps one audit row in the server-streaming
response. The host&#39;s PluginHistoryRouter reads rows from the stream
and adapts them to the eventbus.HistoryStream contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| row | [AuditRow](#holomush-plugin-v1-AuditRow) |  | row is a single audit row from the plugin&#39;s store. Fields match the AuditRow shape used at ingest so the host can reconstruct a full eventbus.Event, including crypto envelope fields for read-back. |






<a name="holomush-plugin-v1-RowResult"></a>

### RowResult
RowResult is the per-row outcome of DecryptOwnAuditRows. Exactly one of
plaintext / no_plaintext_reason is populated: plaintext is set iff the row
decrypted; no_plaintext_reason is set iff the row was refused (e.g.
&#34;not_owner&#34;, &#34;downgrade_refused&#34;, &#34;dek_missing&#34;, &#34;internal&#34;).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [bytes](#bytes) |  | id echoes AuditRow.id so the caller can correlate results back to their source rows without relying solely on positional ordering. |
| plaintext | [bytes](#bytes) |  | plaintext holds the decrypted event payload bytes when decryption succeeded. May be empty bytes for zero-length payloads; callers MUST distinguish this from no_plaintext_reason by which oneof arm is set, not by length. |
| no_plaintext_reason | [string](#string) |  | no_plaintext_reason is a short ASCII token describing why decryption was refused. It is a stable wire contract (the values MUST NOT drift; SDKs switch on them — see readback.go). The full set: &#34;not_owner&#34; (g1 OwnerMap gate — subject belongs to a different plugin), &#34;auth_guard_deny&#34; (recipient not authorized by manifest declaration / ABAC grant — Phase 3b AuthGuard deny), &#34;downgrade_refused&#34; (INV-CRYPTO-42 fence — sensitive event stored under identity codec), &#34;dek_missing&#34; (INV-CRYPTO-50 fence — no DEK exists for this row&#39;s context), &#34;stale_dek&#34; (INV-CRYPTO-108 — both hot and cold DEK tiers gone), &#34;audit_queue_full&#34; (plugin audit-emit backpressure), and &#34;internal&#34; (host-side error, details logged server-side only). |





 

 

 


<a name="holomush-plugin-v1-PluginAuditService"></a>

### PluginAuditService
PluginAuditService is implemented by plugins that declare audit
subjects in their manifest. The host owns the JetStream durable
consumer and forwards each delivered event to the plugin via
AuditEvent. The plugin INSERTs into its own schema and acks.

QueryHistory is invoked by host&#39;s bus.QueryHistory when the queried
subject prefix is owned by this plugin.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| AuditEvent | [AuditEventRequest](#holomush-plugin-v1-AuditEventRequest) | [AuditEventResponse](#holomush-plugin-v1-AuditEventResponse) | AuditEvent is the per-message ingestion RPC. The host per-plugin JetStream consumer calls this for every event delivered on subjects declared in the plugin&#39;s manifest audit block. The plugin MUST INSERT idempotently (ON CONFLICT DO NOTHING) and return a success response; the host then acks the JetStream message. On error the host does NOT nak — JetStream AckWait &#43; MaxDeliver handle retry with natural backoff. The AuditRow payload is forwarded byte-equal (ciphertext is never decrypted before forwarding, INV-CRYPTO-46). |
| QueryHistory | [QueryHistoryRequest](#holomush-plugin-v1-QueryHistoryRequest) | [QueryHistoryResponse](#holomush-plugin-v1-QueryHistoryResponse) stream | QueryHistory streams audit rows for a single subject prefix owned by this plugin. The host&#39;s bus.QueryHistory routes the call here when the OwnerMap maps the requested subject to this plugin. The plugin MUST enforce domain-specific authorization against req.Caller before returning any rows (e.g., scene membership for core-scenes). Rows are ordered by id (ULID lex = chronological) in the direction specified by req.Direction; the page is bounded by req.PageSize (host caps at 200; plugin MUST NOT exceed that cap). |

 



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
CommandRequest carries the full dispatch context for a plugin command
invocation. pluginServerAdapter.HandleCommand maps it to the SDK
CommandRequest type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [string](#string) |  | Parsed command verb the plugin registered (e.g. &#34;say&#34;, &#34;dig&#34;). |
| args | [string](#string) |  | Argument text following the verb (max 8 KiB). |
| raw_input | [string](#string) |  | The raw line the player typed, preserving aliases; surfaced to the SDK as InvokedAs (max 8 KiB). |
| character_id | [string](#string) |  | ULID of the character invoking the command. |
| character_name | [string](#string) |  | Display name of the invoking character. |
| location_id | [string](#string) |  | ULID of the invoking character&#39;s current location. |
| session_id | [string](#string) |  | ULID of the active session the command was issued in. |
| player_id | [string](#string) |  | ULID of the player account behind the character. |
| connection_id | [string](#string) |  | Originating connection ULID (Phase 5). Empty for server-side dispatch paths that do not have a specific connection (e.g., non-gateway callers). |






<a name="holomush-plugin-v1-CommandResponse"></a>

### CommandResponse
CommandResponse is the result of a plugin command execution returned from
HandleCommand.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [CommandStatus](#holomush-plugin-v1-CommandStatus) |  | Outcome category of the command. |
| output | [string](#string) |  | Synchronous text shown to the invoking player (max 8 KiB). |
| events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events the command wants emitted; routed through the host emit fence like HandleEvent emits. |
| audit_hints | [AuditDecisionHint](#holomush-plugin-v1-AuditDecisionHint) | repeated | Audit decision hints accumulated by the plugin handler during this command dispatch. The dispatcher extracts these after the response is returned, stamps host-controlled fields (subject, action base, source, component, timestamp, duration), and flushes them through the audit logger. |






<a name="holomush-plugin-v1-EmitEvent"></a>

### EmitEvent
EmitEvent is one event a plugin wants to emit, returned from HandleEvent /
HandleCommand (the proto mirror of pkg/plugin.EmitEvent). It is NOT published
directly — the host routes it through the PluginEventEmitter.Emit fence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  | Target stream the event is published to (legacy &#34;prefix:id&#34; form). |
| type | [string](#string) |  | Event-type discriminator for the emitted event; gated by the manifest&#39;s emits / crypto.emits declarations at the fence. |
| payload | [string](#string) |  | JSON-encoded payload (max 64 KiB); validated as well-formed JSON at the fence before publish. |
| sensitive | [bool](#bool) |  | Per-event sensitivity claim for a return-value emit, validated against the plugin manifest by event_emitter.go::Emit via EnforceSensitivity (internal/plugin/sensitivity_fence.go) — INV-PLUGIN-29: a sensitivity=never manifest rejects true; INV-PLUGIN-30: a sensitivity=always manifest rejects false. Carries the same semantics as the active EmitEvent RPC&#39;s sensitive field so a binary plugin&#39;s return-value emit cannot silently downgrade to plaintext where the Lua runtime would encrypt (holomush-av954). Default false for backward compatibility. |






<a name="holomush-plugin-v1-Event"></a>

### Event
Event is the host→plugin delivery shape for one game event (the proto mirror
of pkg/plugin.Event). pluginServerAdapter.HandleEvent converts it to the SDK
Event before invoking the author&#39;s handler.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | ULID string uniquely identifying the event; also its bus dedup key. |
| stream | [string](#string) |  | Source stream this event belongs to, in legacy &#34;prefix:id&#34; form (e.g. &#34;location:loc_abc123&#34;). Translated to the dot-delimited NATS subject at the emit boundary. |
| type | [string](#string) |  | Event-type discriminator (e.g. &#34;say&#34;, &#34;pose&#34;, &#34;arrive&#34;, &#34;leave&#34;, &#34;system&#34;) that the plugin handler switches on. |
| timestamp | [int64](#int64) |  | Event occurrence time in Unix milliseconds (host clock). |
| actor_kind | [string](#string) |  | Actor kind as a string (e.g. &#34;character&#34;, &#34;system&#34;, &#34;plugin&#34;). Carried as a string rather than an enum for forward-compat; the SDK maps it to its ActorKind type. Distinct from the bus-internal ActorKind enum. |
| actor_id | [string](#string) |  | ULID of the actor that caused the event (the character/system/plugin id). |
| payload | [string](#string) |  | JSON-encoded event payload (max 64 KiB); the plugin decodes it per the event type&#39;s schema. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination token for this event. Pass as PluginHostServiceQueryStreamHistoryRequest.cursor on the next call to page backward from this position. Empty on events received via delivery (not history). Treat as an opaque blob. |






<a name="holomush-plugin-v1-FocusFailure"></a>

### FocusFailure
FocusFailure carries the connection_id and reason for an AutoFocusOnJoin failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_id | [bytes](#bytes) |  | ULID bytes of the connection that failed to focus. |
| reason | [FocusFailureReason](#holomush-plugin-v1-FocusFailureReason) |  | Why the focus attempt failed for that connection. |






<a name="holomush-plugin-v1-FocusKey"></a>

### FocusKey
FocusKey identifies a focus membership within a session. A session&#39;s
focus memberships are unique by (kind, target_id) pair.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [FocusKind](#holomush-plugin-v1-FocusKind) |  | Which kind of focused context this key names. |
| target_id | [string](#string) |  | ULID of the focused target (e.g. the scene id) within that kind. |






<a name="holomush-plugin-v1-HandleCommandRequest"></a>

### HandleCommandRequest
HandleCommandRequest wraps a command dispatch for the PluginService
HandleCommand call.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [CommandRequest](#holomush-plugin-v1-CommandRequest) |  | The command (verb, args, and dispatch context) to handle. |






<a name="holomush-plugin-v1-HandleCommandResponse"></a>

### HandleCommandResponse
HandleCommandResponse wraps the plugin&#39;s command result.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| response | [CommandResponse](#holomush-plugin-v1-CommandResponse) |  | The command outcome (status, output, response emits, audit hints). |






<a name="holomush-plugin-v1-HandleEventRequest"></a>

### HandleEventRequest
HandleEventRequest wraps a single delivered event for the PluginService
HandleEvent call.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [Event](#holomush-plugin-v1-Event) |  | The event being delivered to the plugin handler. |






<a name="holomush-plugin-v1-HandleEventResponse"></a>

### HandleEventResponse
HandleEventResponse returns the events the plugin chose to emit in reaction
to the delivered event.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| emit_events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events the plugin wants emitted; each is run through the host emit fence. |






<a name="holomush-plugin-v1-InitRequest"></a>

### InitRequest
InitRequest is the host&#39;s first call to a freshly connected plugin process.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [ServiceConfig](#holomush-plugin-v1-ServiceConfig) |  | The initialization payload (DSN, required-service addresses, runtime config) the plugin needs before it can serve events or commands. |






<a name="holomush-plugin-v1-InitResponse"></a>

### InitResponse
InitResponse is the plugin&#39;s reply to Init, advertising what it serves and
what it may emit.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provided_services | [string](#string) | repeated | gRPC service names this plugin implements on the go-plugin transport, so the host&#39;s service registry can route requires→provides between plugins. |
| registered_emit_types | [string](#string) | repeated | Set of plugin-owned event types this plugin may emit. Host validates set-equality against manifest&#39;s crypto.emits per INV-PLUGIN-32. Plugins without crypto.emits leave empty and skip validation; plugins WITH crypto.emits MUST populate (mismatch fails load). |






<a name="holomush-plugin-v1-PluginHostServiceAddSessionStreamRequest"></a>

### PluginHostServiceAddSessionStreamRequest
PluginHostServiceAddSessionStreamRequest is the (currently unserved,
holomush-l6std) request to subscribe an active session to one more stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Active session identifier. |
| stream | [string](#string) |  | Stream name to subscribe to (format: &#34;prefix:id&#34;). |
| replay_mode | [StreamReplayMode](#holomush-plugin-v1-StreamReplayMode) |  | replay_mode controls initial replay. Optional; defaults to FROM_CURSOR if unspecified for backwards compatibility. |






<a name="holomush-plugin-v1-PluginHostServiceAddSessionStreamResponse"></a>

### PluginHostServiceAddSessionStreamResponse
PluginHostServiceAddSessionStreamResponse is the empty ack for
AddSessionStream (unserved, holomush-l6std).






<a name="holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinRequest"></a>

### PluginHostServiceAutoFocusOnJoinRequest
PluginHostServiceAutoFocusOnJoinRequest names the character and scene to
fan-out focus across the character&#39;s connections.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [bytes](#bytes) |  | ULID bytes of the character whose connections are being focused. |
| scene_id | [bytes](#bytes) |  | ULID bytes of the scene to focus those connections on. |






<a name="holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinResponse"></a>

### PluginHostServiceAutoFocusOnJoinResponse
PluginHostServiceAutoFocusOnJoinResponse reports per-connection fan-out
outcomes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focused_connection_ids | [bytes](#bytes) | repeated | Connection ULIDs newly focused on the scene by this call. |
| total_connection_count | [uint32](#uint32) |  | Total terminal/telnet connections the character had (the fan-out denominator). |
| skipped_connection_ids | [bytes](#bytes) | repeated | Connection ULIDs skipped because they were already explicitly focused elsewhere (D8). |
| failed_connection_ids | [FocusFailure](#holomush-plugin-v1-FocusFailure) | repeated | Connections that failed to focus, each with a structured reason. |






<a name="holomush-plugin-v1-PluginHostServiceCommandInfo"></a>

### PluginHostServiceCommandInfo
PluginHostServiceCommandInfo is per-command metadata returned by ListCommands.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the canonical command name (e.g. &#34;scene&#34;). |
| help | [string](#string) |  | help is the one-line description from the command registry. |
| usage | [string](#string) |  | usage is the usage pattern (e.g. &#34;scene &lt;subcommand&gt;&#34;). |
| source | [string](#string) |  | source is &#34;core&#34; or the owning plugin name. |






<a name="holomush-plugin-v1-PluginHostServiceEmitEventRequest"></a>

### PluginHostServiceEmitEventRequest
PluginHostServiceEmitEventRequest is the wire form of a plugin emit. The
caller&#39;s identity is NOT on this message — it is recovered host-side from the
x-holomush-emit-token header (see PluginHostService.EmitEvent).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  | Target stream (legacy &#34;prefix:id&#34; form); its namespace must be declared in the manifest&#39;s emits list or the fence rejects the emit. |
| event_type | [string](#string) |  | Event-type discriminator for the emitted event. |
| payload | [bytes](#bytes) |  | Raw event payload bytes (validated as JSON at the fence). |
| sensitive | [bool](#bool) |  | sensitive declares per-event sensitivity at emit time. Phase 3a&#39;s host-side fence at internal/plugin/event_emitter.go::Emit validates this against the plugin manifest&#39;s declared sensitivity: - manifest sensitivity=never: sensitive=true rejected (INV-PLUGIN-29). - manifest sensitivity=may: sensitive=true/false honored. - manifest sensitivity=always: sensitive=false rejected (INV-PLUGIN-30). Default false (proto3 zero) for older plugins compiled before this field existed — matching pre-Phase-3d behavior. |






<a name="holomush-plugin-v1-PluginHostServiceEmitEventResponse"></a>

### PluginHostServiceEmitEventResponse
PluginHostServiceEmitEventResponse is the empty acknowledgement that an emit
passed the fence and was published.






<a name="holomush-plugin-v1-PluginHostServiceEvaluateRequest"></a>

### PluginHostServiceEvaluateRequest
PluginHostServiceEvaluateRequest names the action and resource to evaluate.
The subject is NOT here — it is recovered host-side from the dispatch token
(spec §2, INV-PLUGIN-22).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action | [string](#string) |  | ABAC action verb to authorize (e.g. &#34;read&#34;, &#34;write&#34;). |
| resource | [string](#string) |  | resource is a typed instance ref: &#34;scene:01ABC...&#34;. |






<a name="holomush-plugin-v1-PluginHostServiceEvaluateResponse"></a>

### PluginHostServiceEvaluateResponse
PluginHostServiceEvaluateResponse returns the ABAC engine&#39;s decision.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| allowed | [bool](#bool) |  | Whether the action is permitted on the resource for the recovered subject. |
| reason | [string](#string) |  | Human-readable rationale for the decision (e.g. the deny reason). |
| matched_policy | [string](#string) |  | Identifier of the policy that produced the decision, when one matched. |






<a name="holomush-plugin-v1-PluginHostServiceGetCommandHelpRequest"></a>

### PluginHostServiceGetCommandHelpRequest
PluginHostServiceGetCommandHelpRequest names a command and the character whose
access is checked before returning detail.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the canonical command name to describe. |
| character_id | [string](#string) |  | character_id is the ULID of the character whose access is checked. |






<a name="holomush-plugin-v1-PluginHostServiceGetCommandHelpResponse"></a>

### PluginHostServiceGetCommandHelpResponse
PluginHostServiceGetCommandHelpResponse returns full help detail.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the canonical command name. |
| help | [string](#string) |  | help is the one-line description. |
| usage | [string](#string) |  | usage is the usage pattern. |
| help_text | [string](#string) |  | help_text is the detailed markdown help body. |
| source | [string](#string) |  | source is &#34;core&#34; or the owning plugin name. |






<a name="holomush-plugin-v1-PluginHostServiceGetConnectionFocusRequest"></a>

### PluginHostServiceGetConnectionFocusRequest
PluginHostServiceGetConnectionFocusRequest carries the connection ID whose
focus is being read. Read-only counterpart of SetConnectionFocus.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_id | [bytes](#bytes) |  | ULID bytes of the connection whose focus is being read. |






<a name="holomush-plugin-v1-PluginHostServiceGetConnectionFocusResponse"></a>

### PluginHostServiceGetConnectionFocusResponse
PluginHostServiceGetConnectionFocusResponse returns the connection&#39;s current
focus key, if any. Absent when the connection is grid-focused or unknown.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focus_key | [FocusKey](#holomush-plugin-v1-FocusKey) | optional | The connection&#39;s focus; absent for grid focus or unknown connection. |






<a name="holomush-plugin-v1-PluginHostServiceGetSettingRequest"></a>

### PluginHostServiceGetSettingRequest
PluginHostServiceGetSettingRequest reads one owner-partitioned key. The owner
is NOT on the wire — the host binds it from the authenticated plugin name
(structural cross-plugin isolation).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scope | [SettingScope](#holomush-plugin-v1-SettingScope) |  | Scope to read from. |
| principal_id | [string](#string) |  | Principal ULID: player ID for PLAYER, character ID for CHARACTER, empty for GAME. |
| key | [string](#string) |  | Plugin-owned dot-key to read (e.g. &#34;content.cw_block&#34;). |






<a name="holomush-plugin-v1-PluginHostServiceGetSettingResponse"></a>

### PluginHostServiceGetSettingResponse
PluginHostServiceGetSettingResponse returns a typed list-or-scalar value read
from the resolved scope/partition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| found | [bool](#bool) |  | Whether the key resolved in the requested scope/partition. |
| string_list | [string](#string) | repeated | String-list value (Phase 8 settings are list-valued). |
| string_value | [string](#string) |  | Scalar string value (for non-list keys). |






<a name="holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedRequest"></a>

### PluginHostServiceIsAnyConnFocusedRequest
PluginHostServiceIsAnyConnFocusedRequest names the character and scene to
test for any focused connection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [bytes](#bytes) |  | ULID bytes of the character to check. |
| scene_id | [bytes](#bytes) |  | ULID bytes of the scene the connections might be focused on. |






<a name="holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedResponse"></a>

### PluginHostServiceIsAnyConnFocusedResponse
PluginHostServiceIsAnyConnFocusedResponse reports the focus check result.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focused | [bool](#bool) |  | True iff at least one of the character&#39;s connections focuses the scene. |






<a name="holomush-plugin-v1-PluginHostServiceJoinFocusRequest"></a>

### PluginHostServiceJoinFocusRequest
PluginHostServiceJoinFocusRequest names the session and the focus target to
add a membership for.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Session to add the focus membership to. |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  | The (kind, target_id) membership to add. |






<a name="holomush-plugin-v1-PluginHostServiceJoinFocusResponse"></a>

### PluginHostServiceJoinFocusResponse
PluginHostServiceJoinFocusResponse is the empty ack that the membership was
added.






<a name="holomush-plugin-v1-PluginHostServiceKVDeleteRequest"></a>

### PluginHostServiceKVDeleteRequest
PluginHostServiceKVDeleteRequest is the (currently unserved, holomush-l6std)
request to delete a key from a plugin&#39;s KV namespace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  | Owning plugin&#39;s name — the KV namespace key. |
| key | [string](#string) |  | Key to delete within that namespace. |






<a name="holomush-plugin-v1-PluginHostServiceKVDeleteResponse"></a>

### PluginHostServiceKVDeleteResponse
PluginHostServiceKVDeleteResponse is the empty ack for KVDelete (unserved,
holomush-l6std).






<a name="holomush-plugin-v1-PluginHostServiceKVGetRequest"></a>

### PluginHostServiceKVGetRequest
PluginHostServiceKVGetRequest is the (currently unserved, holomush-l6std)
request to read a key from a plugin&#39;s KV namespace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  | Owning plugin&#39;s name — the KV namespace key. |
| key | [string](#string) |  | Key to read within that namespace. |






<a name="holomush-plugin-v1-PluginHostServiceKVGetResponse"></a>

### PluginHostServiceKVGetResponse
PluginHostServiceKVGetResponse returns a KV lookup result (unserved,
holomush-l6std).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  | The stored value, or empty when not found. |
| found | [bool](#bool) |  | Whether the key existed; distinguishes a stored empty value from a miss. |






<a name="holomush-plugin-v1-PluginHostServiceKVSetRequest"></a>

### PluginHostServiceKVSetRequest
PluginHostServiceKVSetRequest is the (currently unserved, holomush-l6std)
request to write a key in a plugin&#39;s KV namespace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  | Owning plugin&#39;s name — the KV namespace key. |
| key | [string](#string) |  | Key to write within that namespace. |
| value | [string](#string) |  | Value to store under the key. |






<a name="holomush-plugin-v1-PluginHostServiceKVSetResponse"></a>

### PluginHostServiceKVSetResponse
PluginHostServiceKVSetResponse is the empty ack for KVSet (unserved,
holomush-l6std).






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetRequest"></a>

### PluginHostServiceLeaveFocusByTargetRequest
PluginHostServiceLeaveFocusByTargetRequest names a focus target to remove
from every session that holds it (cross-session fan-out).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  | The (kind, target_id) membership to sweep out of all holding sessions. |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetResponse"></a>

### PluginHostServiceLeaveFocusByTargetResponse
PluginHostServiceLeaveFocusByTargetResponse reports the aggregate result of a
cross-session leave sweep.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| succeeded | [int32](#int32) |  | Number of sessions successfully left. Zero is a valid result (target had no members or every member was already non-a-member — per-session idempotent no-ops count as successes). Callers comparing succeeded &#43; len(failed_session_ids) against total_scanned can distinguish total, partial, and empty-sweep outcomes without parsing any error string. |
| total_scanned | [int32](#int32) |  | Number of non-expired sessions the sweep scanned. Always &gt;= succeeded &#43; len(failed_session_ids). |
| failed_session_ids | [string](#string) | repeated | Session IDs for which the per-session leave failed. Empty means every scanned session succeeded (idempotent no-ops included). Per-session error details are not serialized; callers should treat these IDs as the authoritative partial-failure signal and re-issue LeaveFocus against them if retry is desired. The RPC error is reserved for enumeration/list failures (e.g., the session store could not list members). |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusRequest"></a>

### PluginHostServiceLeaveFocusRequest
PluginHostServiceLeaveFocusRequest names the session and the focus target to
remove a membership for (idempotent on non-member).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Session to remove the focus membership from. |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  | The (kind, target_id) membership to remove. |






<a name="holomush-plugin-v1-PluginHostServiceLeaveFocusResponse"></a>

### PluginHostServiceLeaveFocusResponse
PluginHostServiceLeaveFocusResponse is the empty ack for LeaveFocus.






<a name="holomush-plugin-v1-PluginHostServiceListCommandsRequest"></a>

### PluginHostServiceListCommandsRequest
PluginHostServiceListCommandsRequest names the character whose executable
command set to enumerate.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the ULID of the character whose capabilities filter the list. |






<a name="holomush-plugin-v1-PluginHostServiceListCommandsResponse"></a>

### PluginHostServiceListCommandsResponse
PluginHostServiceListCommandsResponse returns the filtered command set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [PluginHostServiceCommandInfo](#holomush-plugin-v1-PluginHostServiceCommandInfo) | repeated | commands is the ABAC-filtered set the character may execute. |
| incomplete | [bool](#bool) |  | incomplete is true when engine errors hid some commands from the result. |






<a name="holomush-plugin-v1-PluginHostServiceLogRequest"></a>

### PluginHostServiceLogRequest
PluginHostServiceLogRequest is the (currently unserved, holomush-l6std)
request to write one plugin log line through the host logger.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [string](#string) |  | Log severity (e.g. &#34;info&#34;, &#34;warn&#34;, &#34;error&#34;). |
| message | [string](#string) |  | Log message body (max 8 KiB). |






<a name="holomush-plugin-v1-PluginHostServiceLogResponse"></a>

### PluginHostServiceLogResponse
PluginHostServiceLogResponse is the empty ack for Log (unserved,
holomush-l6std).






<a name="holomush-plugin-v1-PluginHostServicePresentFocusRequest"></a>

### PluginHostServicePresentFocusRequest
PluginHostServicePresentFocusRequest names the session and the existing
membership to set as its PresentingFocus.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Session whose PresentingFocus pointer is being set. |
| target | [FocusKey](#holomush-plugin-v1-FocusKey) |  | Existing membership to present; validated against FocusMemberships. |






<a name="holomush-plugin-v1-PluginHostServicePresentFocusResponse"></a>

### PluginHostServicePresentFocusResponse
PluginHostServicePresentFocusResponse is the empty ack for PresentFocus.






<a name="holomush-plugin-v1-PluginHostServiceQueryStreamHistoryRequest"></a>

### PluginHostServiceQueryStreamHistoryRequest
PluginHostServiceQueryStreamHistoryRequest selects a backward-paginated tail
of a stream for plugin-side display.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  | Stream to read history from (legacy &#34;prefix:id&#34; form). |
| count | [int32](#int32) |  | Page size; negative is rejected, values above 500 are clamped to 500. |
| not_before_ms | [int64](#int64) |  | Epoch milliseconds. Events before this time are excluded. 0 means no lower bound. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous response. Events older than the cursor position are returned. Empty = start from latest. |






<a name="holomush-plugin-v1-PluginHostServiceQueryStreamHistoryResponse"></a>

### PluginHostServiceQueryStreamHistoryResponse
PluginHostServiceQueryStreamHistoryResponse returns one history page plus the
cursor for the next (older) page.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [Event](#holomush-plugin-v1-Event) | repeated | The page of events in ascending (oldest→newest) order; each carries its own backward-paging cursor. |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next page. Empty if no more pages. |






<a name="holomush-plugin-v1-PluginHostServiceRemoveSessionStreamRequest"></a>

### PluginHostServiceRemoveSessionStreamRequest
PluginHostServiceRemoveSessionStreamRequest is the (currently unserved,
holomush-l6std) request to unsubscribe an active session from a stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | Active session identifier. |
| stream | [string](#string) |  | Stream name to unsubscribe from (format: &#34;prefix:id&#34;). |






<a name="holomush-plugin-v1-PluginHostServiceRemoveSessionStreamResponse"></a>

### PluginHostServiceRemoveSessionStreamResponse
PluginHostServiceRemoveSessionStreamResponse is the empty ack for
RemoveSessionStream (unserved, holomush-l6std).






<a name="holomush-plugin-v1-PluginHostServiceRequestEmitTokenRequest"></a>

### PluginHostServiceRequestEmitTokenRequest
PluginHostServiceRequestEmitTokenRequest carries no fields. The host
derives the calling plugin&#39;s identity from the mTLS-bound server struct.
Future evolution: do NOT add actor fields here — that would re-open the
G1 forgery surface this RPC is designed to close.






<a name="holomush-plugin-v1-PluginHostServiceRequestEmitTokenResponse"></a>

### PluginHostServiceRequestEmitTokenResponse
PluginHostServiceRequestEmitTokenResponse returns the issued self-token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | Opaque self-token. Plugins MUST treat this as opaque; only the host&#39;s emitTokenStore can interpret it. The token is bound to ActorPlugin &#43; the calling plugin&#39;s name and is single-use-friendly (TTL-revoked). |






<a name="holomush-plugin-v1-PluginHostServiceSetConnectionFocusRequest"></a>

### PluginHostServiceSetConnectionFocusRequest
PluginHostServiceSetConnectionFocusRequest selects one connection and the
focus to set on it (Phase 5).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_id | [bytes](#bytes) |  | ULID bytes of the connection whose focus is being set. |
| focus_key | [FocusKey](#holomush-plugin-v1-FocusKey) | optional | The focus to set; absent (unset) clears the connection&#39;s focus. |
| is_scene_grid | [bool](#bool) |  | is_scene_grid signals that this call originated from a `scene grid` command — substrate skips the D9 PresentingFocus write per D10. |






<a name="holomush-plugin-v1-PluginHostServiceSetConnectionFocusResponse"></a>

### PluginHostServiceSetConnectionFocusResponse
PluginHostServiceSetConnectionFocusResponse echoes the resulting focus.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| focus_key | [FocusKey](#holomush-plugin-v1-FocusKey) | optional | The connection&#39;s focus after the write; absent if the connection was cleared/unfocused. |






<a name="holomush-plugin-v1-PluginHostServiceSetSettingRequest"></a>

### PluginHostServiceSetSettingRequest
PluginHostServiceSetSettingRequest writes one key in the caller&#39;s owner
partition. Owner is bound host-side, not supplied here.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scope | [SettingScope](#holomush-plugin-v1-SettingScope) |  | Target scope to write. |
| principal_id | [string](#string) |  | Principal ULID (empty for GAME). |
| key | [string](#string) |  | Plugin-owned dot-key to write. |
| string_list | [string](#string) | repeated | String-list value to store. |






<a name="holomush-plugin-v1-PluginHostServiceSetSettingResponse"></a>

### PluginHostServiceSetSettingResponse
PluginHostServiceSetSettingResponse is the empty acknowledgement returned on a
successful write.






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
ServiceConfig carries the host→plugin initialization payload delivered in
InitRequest. It is consumed by pluginServerAdapter.Init / the provider&#39;s own
Init.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_string | [string](#string) |  | PostgreSQL DSN the plugin uses for its own storage. Populated only when the plugin declares storage: postgres in its manifest; empty otherwise. |
| required_services | [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry) | repeated | Network addresses of the proto services the plugin declared in requires, keyed by service name, for the plugin to dial. (Reserved for future service-to-service wiring.) |
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
| AUDIT_EFFECT_UNSPECIFIED | 0 | Zero value; never a meaningful plugin decision. |
| AUDIT_EFFECT_DENY | 1 | The plugin denied the action — the hint records why. |
| AUDIT_EFFECT_ALLOW | 2 | The plugin allowed the action. |



<a name="holomush-plugin-v1-CommandStatus"></a>

### CommandStatus
CommandStatus is the outcome category of a plugin command, mirroring
pkg/plugin.CommandStatus.

| Name | Number | Description |
| ---- | ------ | ----------- |
| COMMAND_STATUS_UNSPECIFIED | 0 | Zero value; a well-formed CommandResponse sets a concrete status. |
| COMMAND_STATUS_OK | 1 | The command succeeded. |
| COMMAND_STATUS_ERROR | 2 | The command failed in a recoverable, user-facing way (e.g. bad arguments). |
| COMMAND_STATUS_FAILURE | 3 | The command could not complete due to a runtime failure short of fatal. |
| COMMAND_STATUS_FATAL | 4 | The command hit an unrecoverable condition. |



<a name="holomush-plugin-v1-FocusFailureReason"></a>

### FocusFailureReason
FocusFailureReason is the closed set of per-connection AutoFocusOnJoin
failure causes.

| Name | Number | Description |
| ---- | ------ | ----------- |
| FOCUS_FAILURE_REASON_UNSPECIFIED | 0 | Zero value; not a real failure reason. |
| FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT | 1 | The session lacked the focus membership (JoinFocus was not completed first). |
| FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND | 2 | The connection could not be found (e.g. it dropped during the sweep). |



<a name="holomush-plugin-v1-FocusKind"></a>

### FocusKind
FocusKind enumerates the types of focused contexts a character can
participate in. Adding a new kind requires: (a) a new constant here,
(b) a matching session.FocusKind constant in Go, (c) a new
FocusKindPolicy implementation registered in the coordinator.

| Name | Number | Description |
| ---- | ------ | ----------- |
| FOCUS_KIND_UNSPECIFIED | 0 | Zero value; not a real focus kind — a well-formed FocusKey sets a concrete kind. |
| FOCUS_KIND_SCENE | 1 | A roleplay scene focus. |



<a name="holomush-plugin-v1-SettingScope"></a>

### SettingScope
SettingScope selects which settings scope a Get/Set targets. There is no
chained mode — callers compose scopes themselves (e.g. a CW-block union read
across game&#43;player&#43;character). The iokti.7 handler maps each value to its
backing store and rejects the unspecified value (fail closed).

| Name | Number | Description |
| ---- | ------ | ----------- |
| SETTING_SCOPE_UNSPECIFIED | 0 | Unspecified scope — rejected by the handler (fail closed). |
| SETTING_SCOPE_GAME | 1 | Server-wide scope, backed by holomush_system_info. |
| SETTING_SCOPE_PLAYER | 2 | Per-player scope, backed by players.preferences. |
| SETTING_SCOPE_CHARACTER | 3 | Per-character scope, backed by characters.preferences. |



<a name="holomush-plugin-v1-StreamReplayMode"></a>

### StreamReplayMode
StreamReplayMode controls how a stream subscription&#39;s initial replay
behaves when added via AddSessionStream.

| Name | Number | Description |
| ---- | ------ | ----------- |
| STREAM_REPLAY_MODE_UNSPECIFIED | 0 | Zero value; treated as FROM_CURSOR for backward compatibility. |
| STREAM_REPLAY_MODE_FROM_CURSOR | 1 | Replay from the session&#39;s saved cursor for the stream (catch-up). |
| STREAM_REPLAY_MODE_LIVE_ONLY | 2 | Skip historical replay and deliver only newly-arriving events. |


 

 


<a name="holomush-plugin-v1-PluginHostService"></a>

### PluginHostService
PluginHostService is the host-IMPLEMENTED half of the contract: the server
runs in the host process and binary plugins dial it to call back for event
emission, ABAC evaluation, audit decryption, focus mutation, and history
reads. Registered live at internal/plugin/goplugin/host_service.go
(RegisterPluginHostServiceServer, struct pluginHostServiceServer) — this
surface IS served in production.

NOTE: the registered server embeds UnimplementedPluginHostServiceServer and
implements only 12 of the 18 RPCs below. Log, KVGet, KVSet, KVDelete,
AddSessionStream, and RemoveSessionStream are declared here but have no
server impl and no production client — they return codes.Unimplemented
(tracked in holomush-l6std). Their comments below document that unwired
reality, not aspirational behavior.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| EmitEvent | [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest) | [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse) | EmitEvent publishes one plugin-originated event onto the bus through the host emit fence. SERVED: pluginHostServiceServer.EmitEvent. The caller&#39;s identity is NOT trusted from the wire — the plugin presents a host-issued dispatch token in the x-holomush-emit-token metadata header, the host recovers the vouched-for actor from tokenStore.Lookup(pluginName, token), and a missing/foreign token is rejected (EMIT_TOKEN_MISSING / EMIT_TOKEN_REJECTED). The recovered actor then flows through PluginEventEmitter.Emit, which enforces the manifest gates: emits (subject namespace must be declared), actor_kinds_claimable (actor kind must be listed — EMIT_ACTOR_KIND_NOT_CLAIMABLE), and the crypto.emits sensitivity fence. These gates fire identically for Lua and binary plugins (plugin runtime symmetry); the token mechanism is the binary-side forgery fence. |
| Log | [PluginHostServiceLogRequest](#holomush-plugin-v1-PluginHostServiceLogRequest) | [PluginHostServiceLogResponse](#holomush-plugin-v1-PluginHostServiceLogResponse) | Log forwards a plugin log line to the host logger. DECLARED BUT UNSERVED: pluginHostServiceServer does not implement it and no production client calls it, so it currently returns codes.Unimplemented (holomush-l6std). |
| KVGet | [PluginHostServiceKVGetRequest](#holomush-plugin-v1-PluginHostServiceKVGetRequest) | [PluginHostServiceKVGetResponse](#holomush-plugin-v1-PluginHostServiceKVGetResponse) | KVGet reads a value from the plugin&#39;s namespaced key-value store. DECLARED BUT UNSERVED (holomush-l6std): no server impl, no client; returns codes.Unimplemented today. |
| KVSet | [PluginHostServiceKVSetRequest](#holomush-plugin-v1-PluginHostServiceKVSetRequest) | [PluginHostServiceKVSetResponse](#holomush-plugin-v1-PluginHostServiceKVSetResponse) | KVSet writes a value into the plugin&#39;s namespaced key-value store. DECLARED BUT UNSERVED (holomush-l6std): no server impl, no client; returns codes.Unimplemented today. |
| KVDelete | [PluginHostServiceKVDeleteRequest](#holomush-plugin-v1-PluginHostServiceKVDeleteRequest) | [PluginHostServiceKVDeleteResponse](#holomush-plugin-v1-PluginHostServiceKVDeleteResponse) | KVDelete removes a key from the plugin&#39;s namespaced key-value store. DECLARED BUT UNSERVED (holomush-l6std): no server impl, no client; returns codes.Unimplemented today. |
| AddSessionStream | [PluginHostServiceAddSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceAddSessionStreamRequest) | [PluginHostServiceAddSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceAddSessionStreamResponse) | AddSessionStream subscribes an active session to an additional stream mid-session. DECLARED BUT UNSERVED (holomush-l6std): no server impl, no production client; returns codes.Unimplemented today. (The wire comment about SESSION_NOT_FOUND describes the intended-but-unimplemented contract.) |
| RemoveSessionStream | [PluginHostServiceRemoveSessionStreamRequest](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamRequest) | [PluginHostServiceRemoveSessionStreamResponse](#holomush-plugin-v1-PluginHostServiceRemoveSessionStreamResponse) | RemoveSessionStream unsubscribes an active session from a stream. DECLARED BUT UNSERVED (holomush-l6std): no server impl, no production client; returns codes.Unimplemented today. |
| JoinFocus | [PluginHostServiceJoinFocusRequest](#holomush-plugin-v1-PluginHostServiceJoinFocusRequest) | [PluginHostServiceJoinFocusResponse](#holomush-plugin-v1-PluginHostServiceJoinFocusResponse) | JoinFocus adds a focus membership (e.g. a scene) to a session via the host focus coordinator. SERVED: pluginHostServiceServer.JoinFocus. The plugin declares intent; the coordinator applies the kind-specific replay policy. Fails if the focus coordinator is not configured. |
| LeaveFocus | [PluginHostServiceLeaveFocusRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusRequest) | [PluginHostServiceLeaveFocusResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusResponse) | LeaveFocus removes one focus membership from a session. SERVED: pluginHostServiceServer.LeaveFocus. Idempotent — leaving a target the session does not hold is a successful no-op. |
| LeaveFocusByTarget | [PluginHostServiceLeaveFocusByTargetRequest](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetRequest) | [PluginHostServiceLeaveFocusByTargetResponse](#holomush-plugin-v1-PluginHostServiceLeaveFocusByTargetResponse) | LeaveFocusByTarget removes the given focus membership from every non-expired session that holds it — cross-session fan-out (e.g. a scene-end reaching all participants). SERVED: pluginHostServiceServer.LeaveFocusByTarget. Partial success is normal: per-session failures are aggregated into the response (succeeded / total_scanned / failed_session_ids) rather than aborting the sweep; the RPC error is reserved for the enumeration step itself failing. |
| PresentFocus | [PluginHostServicePresentFocusRequest](#holomush-plugin-v1-PluginHostServicePresentFocusRequest) | [PluginHostServicePresentFocusResponse](#holomush-plugin-v1-PluginHostServicePresentFocusResponse) | PresentFocus repoints a session&#39;s PresentingFocus to an existing membership. SERVED: pluginHostServiceServer.PresentFocus. The target MUST already be in the session&#39;s FocusMemberships (membership is validated, not implicitly created). |
| QueryStreamHistory | [PluginHostServiceQueryStreamHistoryRequest](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryRequest) | [PluginHostServiceQueryStreamHistoryResponse](#holomush-plugin-v1-PluginHostServiceQueryStreamHistoryResponse) | QueryStreamHistory reads the tail of a stream for plugin-side display, backward-paginated by opaque cursor. SERVED: pluginHostServiceServer.QueryStreamHistory via HistoryReader.ReplayTail. Read-only: it does not advance session cursors or mutate session state. A negative count is rejected (INVALID_ARGUMENT); count is CLAMPED to maxQueryStreamHistoryCount (500), not rejected, when too large. |
| DecryptOwnAuditRows | [DecryptOwnAuditRowsRequest](#holomush-plugin-v1-DecryptOwnAuditRowsRequest) | [DecryptOwnAuditRowsResponse](#holomush-plugin-v1-DecryptOwnAuditRowsResponse) | DecryptOwnAuditRows decrypts a batch of the calling plugin&#39;s OWN encrypted audit rows host-side; the plugin never holds a DEK. SERVED: pluginHostServiceServer.DecryptOwnAuditRows via ReadbackDecryptor. DecryptOwnRows. Authorization is two-gate (INV-CRYPTO-27): OwnerMap subject ownership (g1) plus the crypto.emits[].readback manifest flag (g2). Each input row gets an independent RowResult (INV-CRYPTO-37) carrying either plaintext or a stable snake_case no_plaintext_reason (&#34;not_owner&#34;, &#34;auth_guard_deny&#34;, &#34;dek_missing&#34;, &#34;downgrade_refused&#34;, &#34;stale_dek&#34;, &#34;audit_queue_full&#34;, &#34;internal&#34;; readback.go reasonToWire). Request / response shapes and RowResult live in audit.proto (AuditRow domain). |
| RequestEmitToken | [PluginHostServiceRequestEmitTokenRequest](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenRequest) | [PluginHostServiceRequestEmitTokenResponse](#holomush-plugin-v1-PluginHostServiceRequestEmitTokenResponse) | RequestEmitToken issues a self-token bound to {ActorPlugin, pluginName} so a plugin-served gRPC handler (e.g. SceneService.CreateScene) — which is NOT reached via DeliverEvent/DeliverCommand and so holds no dispatch token — can still call EmitEvent. SERVED: pluginHostServiceServer.RequestEmitToken. The plugin&#39;s identity is taken from the mTLS-bound server struct (s.pluginName); the request carries no identity fields, so a plugin cannot impersonate another actor or escalate to a character actor through this RPC. The actor_kinds_claimable manifest gate still fires when the issued token is later spent at EmitEvent. (Spec §3.3.5 / §5.4 two-token pattern.) |
| SetConnectionFocus | [PluginHostServiceSetConnectionFocusRequest](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusRequest) | [PluginHostServiceSetConnectionFocusResponse](#holomush-plugin-v1-PluginHostServiceSetConnectionFocusResponse) | SetConnectionFocus is the Phase-5 explicit focus mutation for a single Connection. SERVED: pluginHostServiceServer.SetConnectionFocus. The substrate validates the requested membership against the session&#39;s FocusMemberships (D4), then writes Connection.FocusKey and (D9-gated) Info.PresentingFocus atomically under one Store-lock acquisition (D7). |
| GetConnectionFocus | [PluginHostServiceGetConnectionFocusRequest](#holomush-plugin-v1-PluginHostServiceGetConnectionFocusRequest) | [PluginHostServiceGetConnectionFocusResponse](#holomush-plugin-v1-PluginHostServiceGetConnectionFocusResponse) | GetConnectionFocus returns the named connection&#39;s current per-connection focus, or absent when the connection is grid-focused (FocusKey nil) or unknown. Read-only counterpart of SetConnectionFocus; lets plugins route connection-scoped operations (e.g. scene pose) to the focused target. See goplugin/host_service.go::GetConnectionFocus. |
| AutoFocusOnJoin | [PluginHostServiceAutoFocusOnJoinRequest](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinRequest) | [PluginHostServiceAutoFocusOnJoinResponse](#holomush-plugin-v1-PluginHostServiceAutoFocusOnJoinResponse) | AutoFocusOnJoin is the Phase-5 fan-out that focuses all of a character&#39;s terminal/telnet connections on a scene at once. SERVED: pluginHostServiceServer.AutoFocusOnJoin. Connections already explicitly focused elsewhere are skipped (D8). The caller MUST have completed JoinFocus first, since the substrate requires the membership to exist. |
| IsAnyConnFocused | [PluginHostServiceIsAnyConnFocusedRequest](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedRequest) | [PluginHostServiceIsAnyConnFocusedResponse](#holomush-plugin-v1-PluginHostServiceIsAnyConnFocusedResponse) | IsAnyConnFocused is the Phase-5 notification-emission helper: it reports whether any of the character&#39;s connections currently focuses the given scene, so callers can decide whether to emit a focus-related notification. SERVED: pluginHostServiceServer.IsAnyConnFocused. |
| Evaluate | [PluginHostServiceEvaluateRequest](#holomush-plugin-v1-PluginHostServiceEvaluateRequest) | [PluginHostServiceEvaluateResponse](#holomush-plugin-v1-PluginHostServiceEvaluateResponse) | Evaluate runs the host ABAC engine for one action against one resource instance owned by the calling plugin. SERVED: pluginHostServiceServer.Evaluate. The subject is derived host-side from the dispatch token exactly as EmitEvent does (token→actor recovery) — there is no subject field on the wire (spec §2, INV-PLUGIN-22). Fails closed on nil engine, missing/rejected token, empty actor subject, or a resource type the plugin does not own. |
| ListCommands | [PluginHostServiceListCommandsRequest](#holomush-plugin-v1-PluginHostServiceListCommandsRequest) | [PluginHostServiceListCommandsResponse](#holomush-plugin-v1-PluginHostServiceListCommandsResponse) | ListCommands enumerates the commands the named character may execute, ABAC-filtered by the host. SERVED: pluginHostServiceServer.ListCommands, delegating to commandquery.Querier.Available. The subject is the request&#39;s character_id (parity with the Lua holomush.list_commands(character_id) host function — not the dispatch-token actor, since this is read-only metadata, not an actor-gated mutation). incomplete is true when engine errors hid some commands. |
| GetCommandHelp | [PluginHostServiceGetCommandHelpRequest](#holomush-plugin-v1-PluginHostServiceGetCommandHelpRequest) | [PluginHostServiceGetCommandHelpResponse](#holomush-plugin-v1-PluginHostServiceGetCommandHelpResponse) | GetCommandHelp returns full help detail for one command after an access check for character_id. SERVED: pluginHostServiceServer.GetCommandHelp, delegating to commandquery.Querier.Help. Mirrors the Lua holomush.get_command_help(name, character_id) host function. |
| GetSetting | [PluginHostServiceGetSettingRequest](#holomush-plugin-v1-PluginHostServiceGetSettingRequest) | [PluginHostServiceGetSettingResponse](#holomush-plugin-v1-PluginHostServiceGetSettingResponse) | GetSetting reads a single-scope setting in the calling plugin&#39;s owner partition (owner bound host-side from the authenticated plugin name, never from the request). The handler resolves SettingScope to its backing store; a missing key returns a successful response with found=false, never a codes.NotFound status error. |
| SetSetting | [PluginHostServiceSetSettingRequest](#holomush-plugin-v1-PluginHostServiceSetSettingRequest) | [PluginHostServiceSetSettingResponse](#holomush-plugin-v1-PluginHostServiceSetSettingResponse) | SetSetting writes a single-scope setting in the calling plugin&#39;s partition; GAME scope requires an operator authorization decision (host-enforced, not trusted from the wire). The owner partition is bound host-side from the authenticated plugin name. |


<a name="holomush-plugin-v1-PluginService"></a>

### PluginService
PluginService is the plugin-IMPLEMENTED half of the binary-plugin contract:
the gRPC server runs inside the plugin subprocess (hashicorp/go-plugin) and
the host dials it. The host adapter is pkg/plugin.pluginServerAdapter, which
bridges these RPCs to a plugin author&#39;s Go handler. Contrast PluginHostService
below, which the host implements and plugins call back into.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Init | [InitRequest](#holomush-plugin-v1-InitRequest) | [InitResponse](#holomush-plugin-v1-InitResponse) | Init is the first call the host makes after the go-plugin handshake. It hands the plugin its ServiceConfig (DB connection string, required-service addresses, opaque runtime config) and returns the gRPC service names the plugin provides plus the emit-type set the host validates against the manifest&#39;s crypto.emits (INV-PLUGIN-32). Bridged by pluginServerAdapter.Init, which also lazily dials the plugin-host connection for any host-facing facade (sink/focus/evaluator/decryptor) the provider opts into. |
| HandleEvent | [HandleEventRequest](#holomush-plugin-v1-HandleEventRequest) | [HandleEventResponse](#holomush-plugin-v1-HandleEventResponse) | HandleEvent delivers one subscribed event to the plugin and collects the events the plugin wants to emit in response. Bridged by pluginServerAdapter.HandleEvent, which converts the proto Event to the SDK Event type, invokes the author&#39;s handler, and converts returned EmitEvents back to the wire. Response emits flow through the host emit fence, not straight to the bus. |
| HandleCommand | [HandleCommandRequest](#holomush-plugin-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-plugin-v1-HandleCommandResponse) | HandleCommand delivers a parsed player command to the plugin and returns the command result (output text, status, response emits, and audit hints). Bridged by pluginServerAdapter.HandleCommand; if the plugin registered no command handler the adapter returns an empty response rather than erroring. |
| QuerySessionStreams | [QuerySessionStreamsRequest](#holomush-plugin-v1-QuerySessionStreamsRequest) | [QuerySessionStreamsResponse](#holomush-plugin-v1-QuerySessionStreamsResponse) | QuerySessionStreams asks the plugin which stream names it wants subscribed for a session being established, before LISTEN/subscription setup. Called exactly once at session establishment, and only for plugins that declare session_streams: true in their manifest. A plugin-reported error degrades gracefully (the host logs and skips that plugin&#39;s contribution). |

 



<a name="holomush_scene_v1_scene-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/scene/v1/scene.proto



<a name="holomush-scene-v1-CastPublishSceneVoteRequest"></a>

### CastPublishSceneVoteRequest
CastPublishSceneVoteRequest records a roster member&#39;s vote on a specific
publication attempt (keyed by published_scene_id, unlike the legacy
scene-keyed CastPublishVoteRequest).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The voting character, who MUST be on the attempt&#39;s frozen roster; required. |
| published_scene_id | [string](#string) |  | The publication attempt being voted on; required. |
| vote | [bool](#bool) |  | The yes (true) / no (false) ballot. |






<a name="holomush-scene-v1-CastPublishSceneVoteResponse"></a>

### CastPublishSceneVoteResponse
CastPublishSceneVoteResponse acknowledges a recorded vote.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| is_change | [bool](#bool) |  | True only when this cast flipped a previously cast, differing vote; the first cast and a re-affirmation of the same value both report false. |






<a name="holomush-scene-v1-CastPublishVoteRequest"></a>

### CastPublishVoteRequest
CastPublishVoteRequest is the legacy (unserved) scene-keyed publish-vote
request, superseded by CastPublishSceneVoteRequest. Retained for the unserved
CastPublishVote RPC&#39;s contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The voting character; required. |
| scene_id | [string](#string) |  | The scene being voted on; required. |
| vote | [bool](#bool) |  | The yes (true) / no (false) ballot. |






<a name="holomush-scene-v1-CastPublishVoteResponse"></a>

### CastPublishVoteResponse
CastPublishVoteResponse is the legacy (unserved) publish-vote acknowledgment.






<a name="holomush-scene-v1-CharacterSceneInfo"></a>

### CharacterSceneInfo
CharacterSceneInfo pairs a scene with the requesting character&#39;s role and
recent-activity metadata for workspace badge rendering.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The scene&#39;s metadata projection via the standard row conversion; the roster fields (participants, observers) are unset on this surface — workspace clients fetch rosters via GetScene/scene-load paths. |
| role | [string](#string) |  | This character&#39;s participant role in the scene (owner/member/observer). |
| last_activity_ms | [int64](#int64) |  | Epoch-ms timestamp of the newest scene_log row on the scene&#39;s IC subject; 0 when the log is empty. |
| entry_count | [int64](#int64) |  | Total scene_log rows on the IC subject (workspace activity panel). |






<a name="holomush-scene-v1-CreateSceneRequest"></a>

### CreateSceneRequest
CreateSceneRequest is the new-scene definition. The calling character becomes
the owner. Empty visibility/pose-order default to &#34;open&#34;/&#34;free&#34; at the
handler; whitespace-only titles are rejected after trimming.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The creating character, who becomes the scene owner; required. |
| title | [string](#string) |  | Scene title; required, 1-200 chars (whitespace-only also rejected by the handler&#39;s post-trim check). |
| description | [string](#string) |  | Optional synopsis, up to 4096 chars. |
| location_id | [string](#string) |  | Optional world location to anchor the scene to. |
| visibility | [string](#string) |  | Discoverability mode; empty selects the &#34;open&#34; default. Constrained to &#34;&#34;|&#34;open&#34;|&#34;private&#34;. |
| pose_order_mode | [string](#string) |  | Pose-order discipline; empty selects the &#34;free&#34; default. Constrained to &#34;&#34;|&#34;free&#34;|&#34;strict&#34;|&#34;3pr&#34;|&#34;5pr&#34;. |
| tags | [string](#string) | repeated | Discovery tags, max 32. |
| content_warnings | [string](#string) | repeated | Content advisories, max 32. |






<a name="holomush-scene-v1-CreateSceneResponse"></a>

### CreateSceneResponse
CreateSceneResponse carries the freshly created scene (active, owner seeded).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The new scene&#39;s projection. |






<a name="holomush-scene-v1-DownloadPublicSceneArchiveRequest"></a>

### DownloadPublicSceneArchiveRequest
DownloadPublicSceneArchiveRequest fetches a published scene rendered to a
file format WITHOUT authentication (status==PUBLISHED gate only).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| published_scene_id | [string](#string) |  | The publication attempt to download; required. Same opacity contract as GetPublicSceneArchiveRequest. |
| format | [string](#string) |  | The render format; required. Supported: &#34;markdown&#34;, &#34;plain_text&#34;, &#34;jsonl&#34;. |






<a name="holomush-scene-v1-DownloadPublicSceneArchiveResponse"></a>

### DownloadPublicSceneArchiveResponse
DownloadPublicSceneArchiveResponse carries the rendered public-archive bytes
and their MIME type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | The rendered file content. |
| mime_type | [string](#string) |  | The content&#39;s MIME type. |






<a name="holomush-scene-v1-DownloadPublishedSceneRequest"></a>

### DownloadPublishedSceneRequest
DownloadPublishedSceneRequest fetches a PUBLISHED attempt rendered to a file
format, as a participant (participant-gated, INV-SCENE-60).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The downloading character, who MUST be a participant; required. |
| published_scene_id | [string](#string) |  | The PUBLISHED attempt to download; required. |
| format | [string](#string) |  | The render format; required. Supported: &#34;markdown&#34;, &#34;plain_text&#34;, &#34;jsonl&#34; (publishRenderMime in publish_service.go); any other value is rejected. |






<a name="holomush-scene-v1-DownloadPublishedSceneResponse"></a>

### DownloadPublishedSceneResponse
DownloadPublishedSceneResponse carries the rendered scene bytes and their
MIME type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | The rendered file content. |
| mime_type | [string](#string) |  | The content&#39;s MIME type (text/markdown, text/plain, or application/jsonl). |






<a name="holomush-scene-v1-EndSceneRequest"></a>

### EndSceneRequest
EndSceneRequest identifies the scene to end and the acting character (the
owner, per the ABAC end-own-scene policy).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The acting character&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to end; required. |






<a name="holomush-scene-v1-EndSceneResponse"></a>

### EndSceneResponse
EndSceneResponse carries the scene as of the ended transition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The post-transition scene row (state == &#34;ended&#34;). |






<a name="holomush-scene-v1-ExportSceneLogRequest"></a>

### ExportSceneLogRequest
ExportSceneLogRequest asks for a scene&#39;s IC log rendered to a downloadable
document. Participant-gated in plugin code (any role); see ExportSceneLog.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The exporting character; required; must hold a participant row (any role). |
| scene_id | [string](#string) |  | The scene to export; required. Works for active, paused, and ended scenes. |
| format | [string](#string) |  | The render format; required: &#34;markdown&#34; or &#34;jsonl&#34;. |






<a name="holomush-scene-v1-ExportSceneLogResponse"></a>

### ExportSceneLogResponse
ExportSceneLogResponse carries the rendered scene-log document and download
metadata.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | The rendered document bytes. |
| mime_type | [string](#string) |  | The content&#39;s MIME type (text/markdown or application/jsonl) — mirrors DownloadPublishedScene&#39;s MIME vocabulary. |
| filename | [string](#string) |  | Suggested download filename (slugified title &#43; extension). |






<a name="holomush-scene-v1-ExtendScenePublishVoteAttemptsRequest"></a>

### ExtendScenePublishVoteAttemptsRequest
ExtendScenePublishVoteAttemptsRequest raises a scene&#39;s publish-attempt budget
(admin-only, ABAC-gated at dispatch).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The acting admin&#39;s character ID; required (admin authority is ABAC-gated, not checked in-plugin). |
| scene_id | [string](#string) |  | The scene whose budget to raise; required. |
| additional | [int32](#int32) |  | How many additional attempts to grant; MUST be positive. |






<a name="holomush-scene-v1-ExtendScenePublishVoteAttemptsResponse"></a>

### ExtendScenePublishVoteAttemptsResponse
ExtendScenePublishVoteAttemptsResponse reports the scene&#39;s new attempt
budget.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| new_max | [int32](#int32) |  | The scene&#39;s max-publish-attempts budget after the extension. |






<a name="holomush-scene-v1-GetPoseOrderRequest"></a>

### GetPoseOrderRequest
GetPoseOrderRequest identifies the scene whose pose order is requested and
the requesting character (who MUST be a participant; the gate is plugin-code,
not ABAC).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The requesting character; MUST be an owner or member of the scene. |
| scene_id | [string](#string) |  | The scene to compute pose order for; required. |






<a name="holomush-scene-v1-GetPoseOrderResponse"></a>

### GetPoseOrderResponse
GetPoseOrderResponse carries the scene&#39;s pose-order mode and the computed
per-participant standings.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mode | [string](#string) |  | The scene&#39;s pose-order mode: &#34;strict&#34;, &#34;3pr&#34;, &#34;5pr&#34;, or &#34;free&#34;. |
| total_pose_count | [uint32](#uint32) |  | Total poses recorded in the scene (the rolling denominator for the poses_since_last gaps). |
| entries | [PoseOrderEntry](#holomush-scene-v1-PoseOrderEntry) | repeated | Per-participant pose-order standings. |






<a name="holomush-scene-v1-GetPublicSceneArchiveRequest"></a>

### GetPublicSceneArchiveRequest
GetPublicSceneArchiveRequest reads a published scene WITHOUT authentication.
No caller identity is required — the only gate is status==PUBLISHED.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| published_scene_id | [string](#string) |  | The publication attempt to read; required. A missing id or any non-PUBLISHED attempt returns one opaque NOT_FOUND (INV-SCENE-35). |






<a name="holomush-scene-v1-GetPublicSceneArchiveResponse"></a>

### GetPublicSceneArchiveResponse
GetPublicSceneArchiveResponse is the public-safe view of a published scene —
only the published artifact, never vote state, per-voter data, or
failure_reason (the §5.1 two-pair separation).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | The publication attempt&#39;s ID. |
| title_snapshot | [string](#string) |  | The scene title snapshotted at publish time. |
| participants_snapshot | [string](#string) | repeated | The participant character names snapshotted at publish time. |
| content_entries | [PublishedSceneEntry](#holomush-scene-v1-PublishedSceneEntry) | repeated | The frozen published content. |
| published_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond publish time. |






<a name="holomush-scene-v1-GetPublishedSceneRequest"></a>

### GetPublishedSceneRequest
GetPublishedSceneRequest reads a publication attempt&#39;s full state as a scene
participant (participant-gated, INV-SCENE-60).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The reading character, who MUST be a participant of the scene; required. |
| published_scene_id | [string](#string) |  | The publication attempt to read; required. |






<a name="holomush-scene-v1-GetPublishedSceneResponse"></a>

### GetPublishedSceneResponse
GetPublishedSceneResponse is the participant-visible view of a publication
attempt: its state-machine status, vote tally, snapshots, lifecycle
timestamps, and (only when PUBLISHED) its frozen content.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | The publication attempt&#39;s ID. |
| scene_id | [string](#string) |  | The scene this attempt belongs to. |
| attempt_number | [int32](#int32) |  | The attempt&#39;s ordinal within the scene&#39;s budget (1-based). |
| status | [string](#string) |  | State-machine status: &#34;COLLECTING&#34;, &#34;COOLOFF&#34;, &#34;PUBLISHED&#34;, or &#34;ATTEMPT_FAILED&#34; (PublishedSceneStatus in publish_types.go). |
| failure_reason | [string](#string) |  | The failure cause; empty unless status is ATTEMPT_FAILED. One of ANY_NO, TIMEOUT, WITHDRAWN, SNAPSHOT_DECRYPT_FAILED, SNAPSHOT_RENDER_FAILED, or COOLOFF_INVARIANT_BROKEN (PublishFailureReason in publish_types.go). |
| tally | [PublishedSceneVoteSummary](#holomush-scene-v1-PublishedSceneVoteSummary) |  | The current yes/no/pending vote tally. |
| content_entries | [PublishedSceneEntry](#holomush-scene-v1-PublishedSceneEntry) | repeated | The frozen published content; populated ONLY when status is PUBLISHED. |
| title_snapshot | [string](#string) |  | The scene title snapshotted at publish time. |
| participants_snapshot | [string](#string) | repeated | The participant character names snapshotted at publish time. |
| initiated_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the attempt was opened. |
| cooloff_started_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the cool-off window began; 0 if cool-off never started. |
| resolved_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the attempt reached a terminal status; 0 if still active. |
| published_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the attempt was published; 0 unless PUBLISHED. |






<a name="holomush-scene-v1-GetSceneRequest"></a>

### GetSceneRequest
GetSceneRequest identifies the scene to read and the character reading it
(the latter scoping the host&#39;s ABAC read policy).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The reading character&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to load; required. |






<a name="holomush-scene-v1-GetSceneResponse"></a>

### GetSceneResponse
GetSceneResponse carries the requested scene&#39;s full projection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The loaded scene; unset is never returned (a miss is codes.NotFound). |






<a name="holomush-scene-v1-InviteToSceneRequest"></a>

### InviteToSceneRequest
InviteToSceneRequest identifies the inviting owner, the scene, and the
character being granted an invitation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The inviting character (the owner per ABAC); required. |
| scene_id | [string](#string) |  | The scene to invite into; required. |
| target_character_id | [string](#string) |  | The character receiving the invitation; required. |






<a name="holomush-scene-v1-InviteToSceneResponse"></a>

### InviteToSceneResponse
InviteToSceneResponse is intentionally empty — a successful invite carries
no body.






<a name="holomush-scene-v1-JoinSceneRequest"></a>

### JoinSceneRequest
JoinSceneRequest identifies the scene to join and the joining character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The joining character&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to join; required. |






<a name="holomush-scene-v1-JoinSceneResponse"></a>

### JoinSceneResponse
JoinSceneResponse is intentionally empty — a successful join carries no
body; the caller refetches scene state if needed.






<a name="holomush-scene-v1-KickFromSceneRequest"></a>

### KickFromSceneRequest
KickFromSceneRequest identifies the acting owner, the scene, and the target
character to remove.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The acting character (the owner per ABAC); required. |
| scene_id | [string](#string) |  | The scene to remove the target from; required. |
| target_character_id | [string](#string) |  | The character to remove (must not be the owner); required. |






<a name="holomush-scene-v1-KickFromSceneResponse"></a>

### KickFromSceneResponse
KickFromSceneResponse is intentionally empty — a successful kick carries no
body.






<a name="holomush-scene-v1-LeaveSceneRequest"></a>

### LeaveSceneRequest
LeaveSceneRequest identifies the scene to leave and the leaving character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The leaving character&#39;s ID; required (must not be the owner). |
| scene_id | [string](#string) |  | The scene to leave; required. |






<a name="holomush-scene-v1-LeaveSceneResponse"></a>

### LeaveSceneResponse
LeaveSceneResponse is intentionally empty — a successful leave carries no
body.






<a name="holomush-scene-v1-ListCharacterScenesRequest"></a>

### ListCharacterScenesRequest
ListCharacterScenesRequest requests the non-archived scene participations
for a single character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The character whose participations to list; required (host-trusted). |






<a name="holomush-scene-v1-ListCharacterScenesResponse"></a>

### ListCharacterScenesResponse
ListCharacterScenesResponse carries the character&#39;s scene participations,
most recently active first.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scenes | [CharacterSceneInfo](#holomush-scene-v1-CharacterSceneInfo) | repeated | The character&#39;s scenes, most recently active first. |






<a name="holomush-scene-v1-ListPublishedScenesRequest"></a>

### ListPublishedScenesRequest
ListPublishedScenesRequest pages through PUBLISHED scene archives.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| limit | [int32](#int32) |  | Page size; 0 means server default, capped at 200 (mirrors ListScenes). |
| offset | [int32](#int32) |  | Leading results to skip. |
| tags | [string](#string) | repeated | Restrict to archives whose scene carries all of these tags. |






<a name="holomush-scene-v1-ListPublishedScenesResponse"></a>

### ListPublishedScenesResponse
ListPublishedScenesResponse carries the public-safe archive summaries,
newest first.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| archives | [PublicSceneArchive](#holomush-scene-v1-PublicSceneArchive) | repeated | Public-safe published-archive summaries, newest first. |






<a name="holomush-scene-v1-ListScenePublishAttemptsRequest"></a>

### ListScenePublishAttemptsRequest
ListScenePublishAttemptsRequest lists a scene&#39;s publication attempts as a
participant (participant-gated, INV-SCENE-60).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The reading character, who MUST be a participant; required. |
| scene_id | [string](#string) |  | The scene whose attempts to list; required. |






<a name="holomush-scene-v1-ListScenePublishAttemptsResponse"></a>

### ListScenePublishAttemptsResponse
ListScenePublishAttemptsResponse carries the attempt summaries (header only,
no content), ordered by attempt number.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempts | [PublishedSceneSummary](#holomush-scene-v1-PublishedSceneSummary) | repeated | The scene&#39;s publication attempts. |






<a name="holomush-scene-v1-ListScenesRequest"></a>

### ListScenesRequest
ListScenesRequest is the scene-board discovery query. It supports pagination,
tag filtering, and content-warning exclusion via the union of scope-based
cw_block settings, served by SceneServiceImpl.ListScenes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| limit | [int32](#int32) |  | Maximum scenes to return; 0 means server default, capped at 200. |
| offset | [int32](#int32) |  | Number of leading results to skip for pagination. |
| tags | [string](#string) | repeated | Restrict results to scenes carrying all of these tags. |
| exclude_content_warnings | [string](#string) | repeated | Extra content-warning categories to hide for this one query, on top of the caller&#39;s stored block. The board query unions these with the game/player/character content.cw_block lists and drops any scene tagged with a blocked category. Applied server-side so the page limit/offset stay correct after filtering. |
| character_id | [string](#string) |  | Acting character ULID. The board query reads this principal&#39;s character-scope content.cw_block to assemble the effective block set. Optional — omit to browse without character-scope CW filtering. |
| player_id | [string](#string) |  | Owning player ULID. The board query reads this principal&#39;s player-scope content.cw_block, which applies across all of the player&#39;s characters, into the effective block set. Optional — omit to browse without player-scope CW filtering. |






<a name="holomush-scene-v1-ListScenesResponse"></a>

### ListScenesResponse
ListScenesResponse is the scene-board discovery result page.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scenes | [SceneInfo](#holomush-scene-v1-SceneInfo) | repeated | The matching scenes for this page. |






<a name="holomush-scene-v1-ParticipantInfo"></a>

### ParticipantInfo
ParticipantInfo is one entry in a scene&#39;s roster — a character&#39;s relationship
to the scene at read time.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Character ID of the participant. |
| character_name | [string](#string) |  | Display name of the character (best-effort; falls back to the ID when no name resolver is wired). |
| role | [string](#string) |  | Participant role: &#34;owner&#34;, &#34;member&#34;, or the transient &#34;invited&#34; (the last exists only on private scenes and is promoted to member on join). See ParticipantRole in participants.go. |
| joined_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | When the participant joined (for invited rows, when the invitation was recorded; reset to join time on promotion). |






<a name="holomush-scene-v1-PauseSceneRequest"></a>

### PauseSceneRequest
PauseSceneRequest identifies the scene to pause and the acting owner.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The acting character&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to pause; required. |






<a name="holomush-scene-v1-PauseSceneResponse"></a>

### PauseSceneResponse
PauseSceneResponse carries the scene as of the paused transition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The post-transition scene row (state == &#34;paused&#34;). |






<a name="holomush-scene-v1-PoseOrderEntry"></a>

### PoseOrderEntry
PoseOrderEntry is one participant&#39;s standing in the computed pose order,
produced by poseorder.go::Compute.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The participant&#39;s character ID. |
| character_name | [string](#string) |  | The participant&#39;s display name (falls back to the character ID when no name resolver is wired). |
| eligible | [bool](#bool) |  | Whether this participant is currently eligible to pose under the scene&#39;s pose-order mode. |
| last_posed_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | When the participant last posed in this scene; unset if they never have. |
| poses_since_last | [uint32](#uint32) | optional | Count of poses by other characters since this participant&#39;s last pose (or since scene start if never posed). Meaningful for 3pr/5pr modes. |






<a name="holomush-scene-v1-PublicSceneArchive"></a>

### PublicSceneArchive
PublicSceneArchive is the public-safe view of a published scene archive,
carrying only the published artifact — never vote state, per-voter data, or
failure_reason. Covers the GetPublicSceneArchive field set (INV-SCENE-35
status gate) plus current scene tags, which that response does not carry.
Used as the element type in ListPublishedScenes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | The publication attempt&#39;s ID. |
| title_snapshot | [string](#string) |  | The scene title snapshotted at publish time. |
| participants_snapshot | [string](#string) | repeated | The participant character names snapshotted at publish time. |
| content_entries | [PublishedSceneEntry](#holomush-scene-v1-PublishedSceneEntry) | repeated | The frozen published content entries. |
| published_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond publish time. |
| tags | [string](#string) | repeated | The CURRENT tags on the source scene row (read at list time, not snapshotted at publish): post-publish tag edits are reflected here. Tags are public metadata (already exposed by ListScenes). |






<a name="holomush-scene-v1-PublishedSceneEntry"></a>

### PublishedSceneEntry
PublishedSceneEntry is one rendered line of a published scene&#39;s frozen
content. Only IC pose/say/emit content survives into a published scene; OOC
and ops events are excluded (EntryKind in publish_types.go).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| speaker | [string](#string) |  | The speaking character&#39;s display label for this line. |
| kind | [string](#string) |  | The content kind: &#34;pose&#34;, &#34;say&#34;, or &#34;emit&#34;. |
| content | [string](#string) |  | The rendered line content. |






<a name="holomush-scene-v1-PublishedSceneSummary"></a>

### PublishedSceneSummary
PublishedSceneSummary is the content-free header view of one publication
attempt, used in the audit list.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | The attempt&#39;s ID. |
| attempt_number | [int32](#int32) |  | The attempt&#39;s ordinal within the scene&#39;s budget (1-based). |
| status | [string](#string) |  | State-machine status (COLLECTING/COOLOFF/PUBLISHED/ATTEMPT_FAILED). |
| failure_reason | [string](#string) |  | Failure cause; empty unless status is ATTEMPT_FAILED. |
| initiated_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the attempt was opened. |
| resolved_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond time the attempt resolved; 0 if still active. |






<a name="holomush-scene-v1-PublishedSceneVoteSummary"></a>

### PublishedSceneVoteSummary
PublishedSceneVoteSummary is the yes/no/pending tally across a publication
attempt&#39;s frozen roster (VoteTally in publish_store.go). Pending counts
roster members who have not yet cast.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| yes | [int32](#int32) |  | Number of yes votes cast. |
| no | [int32](#int32) |  | Number of no votes cast. |
| pending | [int32](#int32) |  | Number of roster members who have not yet voted. |






<a name="holomush-scene-v1-ResumeSceneRequest"></a>

### ResumeSceneRequest
ResumeSceneRequest identifies the scene to resume and the acting owner.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The acting character&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to resume; required. |






<a name="holomush-scene-v1-ResumeSceneResponse"></a>

### ResumeSceneResponse
ResumeSceneResponse carries the scene as of the resumed transition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The post-transition scene row (state == &#34;active&#34;). |






<a name="holomush-scene-v1-SceneInfo"></a>

### SceneInfo
SceneInfo is the wire projection of a scene row plus its roster, returned by
the read and lifecycle RPCs (rowToProto in service.go). The state,
pose_order_mode, and visibility fields are the plugin&#39;s lowercase string
enums, not proto enums.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Scene primary key (&#34;scene-&lt;ULID&gt;&#34;), stable for the scene&#39;s lifetime. |
| title | [string](#string) |  | Human-facing scene name; trimmed of surrounding whitespace at creation. |
| description | [string](#string) |  | Optional free-text scene synopsis (up to 4096 chars). |
| location_id | [string](#string) |  | Optional world location the scene is anchored to; empty when unanchored. |
| owner_id | [string](#string) |  | Character ID of the current owner — the sole authority for end/pause/ resume/update/invite/kick and the only member who cannot leave. |
| state | [string](#string) |  | Lifecycle state: one of &#34;active&#34;, &#34;paused&#34;, &#34;ended&#34;, or &#34;archived&#34; (see SceneState in types.go). Transitions are forward-only. |
| pose_order_mode | [string](#string) |  | Pose-order discipline: one of &#34;free&#34;, &#34;strict&#34;, &#34;3pr&#34;, or &#34;5pr&#34; (see PoseOrderMode in types.go); governs GetPoseOrder eligibility computation. |
| content_warnings | [string](#string) | repeated | Operator-facing content advisories for the scene (max 32 entries). |
| tags | [string](#string) | repeated | Discovery/categorization tags (max 32 entries). |
| visibility | [string](#string) |  | Discoverability mode: &#34;open&#34; (listed, any character may join) or &#34;private&#34; (unlisted, invitation required). See SceneVisibility in types.go. |
| created_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | Wall-clock creation time. For CreateScene responses this is the host clock at create; for GetScene it is the persisted row timestamp. |
| ended_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | Set only once the scene has reached the ended state; otherwise unset. |
| participants | [ParticipantInfo](#holomush-scene-v1-ParticipantInfo) | repeated | The current participant roster (owners and members; invited rows are not surfaced as participants here). |
| observers | [ParticipantInfo](#holomush-scene-v1-ParticipantInfo) | repeated | The watching (role=observer) participants, listed separately from the acting roster and excluded from pose order and publish votes (INV-SCENE-61). Not yet populated by any RPC; the scene-watch read path (E9.5 Task 3&#43;) fills it from the store&#39;s observers query. |






<a name="holomush-scene-v1-ScenePublishCoolOffStartedEvent"></a>

### ScenePublishCoolOffStartedEvent
ScenePublishCoolOffStartedEvent announces that an attempt entered the
cool-off window (all roster members voted yes). Emitted as
scene_publish_cooloff_started.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_id | [string](#string) |  | The publication attempt&#39;s ID. |
| cooloff_ends_at_unix_ns | [int64](#int64) |  | Epoch-nanosecond deadline at which cool-off ends (derived from the persisted cool-off start plus the window, for retry determinism). |






<a name="holomush-scene-v1-ScenePublishResolvedEvent"></a>

### ScenePublishResolvedEvent
ScenePublishResolvedEvent announces that an attempt reached a terminal status
(PUBLISHED or ATTEMPT_FAILED). Emitted as scene_publish_resolved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_id | [string](#string) |  | The publication attempt&#39;s ID. |
| outcome | [string](#string) |  | The terminal outcome: &#34;PUBLISHED&#34; or &#34;ATTEMPT_FAILED&#34;. |
| failure_reason | [string](#string) |  | The failure cause; empty unless outcome is ATTEMPT_FAILED. |
| tally_yes | [int32](#int32) |  | Final yes-vote count. |
| tally_no | [int32](#int32) |  | Final no-vote count. |
| tally_pending | [int32](#int32) |  | Final pending (never-cast) count. |






<a name="holomush-scene-v1-ScenePublishStartedEvent"></a>

### ScenePublishStartedEvent
ScenePublishStartedEvent announces a newly opened publication attempt and its
frozen vote roster. Emitted as scene_publish_started.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_id | [string](#string) |  | The publication attempt&#39;s ID. |
| attempt_number | [int32](#int32) |  | The attempt&#39;s ordinal within the scene&#39;s budget (1-based). |
| initiated_by | [string](#string) |  | Character ID that initiated the attempt. |
| vote_window_seconds | [int64](#int64) |  | The voting-window duration in seconds. |
| cooloff_window_seconds | [int64](#int64) |  | The cool-off-window duration in seconds. |
| roster_character_ids | [string](#string) | repeated | The frozen voter roster (character IDs eligible to vote on this attempt). |






<a name="holomush-scene-v1-ScenePublishVoteAttemptsExtendedEvent"></a>

### ScenePublishVoteAttemptsExtendedEvent
ScenePublishVoteAttemptsExtendedEvent announces an admin raising a scene&#39;s
publish-attempt budget. Emitted as scene_publish_vote_attempts_extended.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene_id | [string](#string) |  | The scene whose budget was raised. |
| additional | [int32](#int32) |  | The number of additional attempts granted. |
| new_max | [int32](#int32) |  | The scene&#39;s new max-publish-attempts budget. |
| admin_id | [string](#string) |  | Character ID of the admin who extended the budget. |






<a name="holomush-scene-v1-ScenePublishVoteCastEvent"></a>

### ScenePublishVoteCastEvent
ScenePublishVoteCastEvent announces a vote cast on a publication attempt.
Emitted as scene_publish_vote_cast.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_id | [string](#string) |  | The publication attempt&#39;s ID. |
| character_id | [string](#string) |  | The voting character&#39;s ID. |
| vote | [bool](#bool) |  | The yes (true) / no (false) ballot just recorded. |
| is_change | [bool](#bool) |  | True only when this cast flipped a previously cast, differing vote. |






<a name="holomush-scene-v1-ScenePublishWithdrawnEvent"></a>

### ScenePublishWithdrawnEvent
ScenePublishWithdrawnEvent announces that the scene owner withdrew an active
attempt (a companion to the resolved event so renderers can distinguish a
withdrawal from a vote failure). Emitted as scene_publish_withdrawn.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempt_id | [string](#string) |  | The publication attempt&#39;s ID. |
| withdrawn_by | [string](#string) |  | Character ID of the owner who withdrew the attempt. |






<a name="holomush-scene-v1-StartScenePublishRequest"></a>

### StartScenePublishRequest
StartScenePublishRequest opens a publication attempt for an ended scene.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The character initiating the attempt; required. |
| scene_id | [string](#string) |  | The ended scene to publish; required. |






<a name="holomush-scene-v1-StartScenePublishResponse"></a>

### StartScenePublishResponse
StartScenePublishResponse identifies the newly created publication attempt.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| published_scene_id | [string](#string) |  | ID of the new publication attempt row (the published_scene_id used by the vote/withdraw/read RPCs). |
| attempt_number | [int32](#int32) |  | The attempt&#39;s ordinal within the scene&#39;s attempt budget (1-based). |






<a name="holomush-scene-v1-TransferOwnershipRequest"></a>

### TransferOwnershipRequest
TransferOwnershipRequest identifies the current owner, the scene, and the
member who will become the new owner.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The current owner; required. |
| scene_id | [string](#string) |  | The scene whose ownership transfers; required. |
| new_owner_character_id | [string](#string) |  | The new owner, who MUST already be a member of the scene; required. |






<a name="holomush-scene-v1-TransferOwnershipResponse"></a>

### TransferOwnershipResponse
TransferOwnershipResponse is intentionally empty — a successful transfer
carries no body.






<a name="holomush-scene-v1-UpdateSceneRequest"></a>

### UpdateSceneRequest
UpdateSceneRequest applies a partial update to mutable scene metadata using
google.protobuf.FieldMask as the canonical proto3 partial-update pattern (per
Google AIP-134). The mask is the single source of truth for &#34;which fields to
apply&#34; — fields listed in the mask are updated to the request value (even if
empty/zero); fields not in the mask are left unchanged.

Per-field constraint semantics:
- max_len limits apply to all string fields regardless of mask membership
- min_len IS NOT used at the proto layer because the mask gates whether
  the field is applied; per-field semantic validation (e.g. &#34;title cannot
  be empty when in the mask&#34;) happens in the service handler&#39;s mask-iteration
  switch statement (buildSceneUpdate in service.go)
- enum-style fields use `in:` constraints that include the empty string
  so that &#34;field not set&#34; doesn&#39;t trip the validator


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The acting owner&#39;s ID; required. |
| scene_id | [string](#string) |  | The scene to update; required. |
| title | [string](#string) |  | New title (applied only when &#34;title&#34; is in the mask; whitespace-only is rejected by the handler). Max 200 chars. |
| description | [string](#string) |  | New description (applied only when &#34;description&#34; is in the mask; empty clears it). Max 4096 chars. |
| visibility | [string](#string) |  | New visibility (applied only when &#34;visibility&#34; is in the mask; empty is rejected by the handler when masked). Constrained to &#34;&#34;|&#34;open&#34;|&#34;private&#34;. |
| pose_order_mode | [string](#string) |  | New pose-order mode (applied only when &#34;pose_order_mode&#34; is in the mask; empty is rejected when masked). A real change auto-emits a pose-order- changed IC notice. Constrained to &#34;&#34;|&#34;free&#34;|&#34;strict&#34;|&#34;3pr&#34;|&#34;5pr&#34;. |
| location_id | [string](#string) |  | New location anchor (applied only when &#34;location_id&#34; is in the mask; empty clears the anchor). |
| content_warnings | [string](#string) | repeated | Replacement content warnings (applied only when &#34;content_warnings&#34; is in the mask). Max 32. |
| tags | [string](#string) | repeated | Replacement tags (applied only when &#34;tags&#34; is in the mask). Max 32. |
| update_mask | [google.protobuf.FieldMask](https://protobuf.dev/reference/protobuf/google.protobuf/#fieldmask) |  | The set of field paths to apply. An empty mask is a no-op success. |






<a name="holomush-scene-v1-UpdateSceneResponse"></a>

### UpdateSceneResponse
UpdateSceneResponse carries the scene after the partial update.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [SceneInfo](#holomush-scene-v1-SceneInfo) |  | The post-update scene row. |






<a name="holomush-scene-v1-WatchSceneRequest"></a>

### WatchSceneRequest
WatchSceneRequest identifies the watcher, target scene, and the watcher&#39;s
game session (host-supplied; the session receives the FocusMembership).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | The watching character&#39;s ID; required. Trusted host-supplied identity per the SceneService caller contract (service.go&#39;s ABAC note). |
| scene_id | [string](#string) |  | The scene to watch; required. Must be visibility=open and active/paused. |
| session_id | [string](#string) |  | The watcher&#39;s game session ULID; required — JoinFocus registers the scene FocusMembership on this session. |






<a name="holomush-scene-v1-WatchSceneResponse"></a>

### WatchSceneResponse
WatchSceneResponse confirms the observer row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| participant | [ParticipantInfo](#holomush-scene-v1-ParticipantInfo) |  | The resulting participant entry (role=observer, or the pre-existing row when the character was already a participant of any role). |






<a name="holomush-scene-v1-WithdrawScenePublishRequest"></a>

### WithdrawScenePublishRequest
WithdrawScenePublishRequest abandons an active publication attempt (owner
only).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| caller_character_id | [string](#string) |  | The acting character, who MUST be the scene owner; required. |
| published_scene_id | [string](#string) |  | The active attempt to withdraw; required. |






<a name="holomush-scene-v1-WithdrawScenePublishResponse"></a>

### WithdrawScenePublishResponse
WithdrawScenePublishResponse is intentionally empty — a successful withdrawal
carries no body; the attempt transitions to ATTEMPT_FAILED(WITHDRAWN).





 

 

 


<a name="holomush-scene-v1-SceneService"></a>

### SceneService
SceneService is the gRPC contract for the core-scenes binary plugin
(plugins/core-scenes/). A scene is a structured roleplay encounter with an
owner, a participant roster, a privacy mode, a pose-order discipline, and an
optional published archive. The plugin owns the `scene` ABAC resource type
and persists to its own `plugin_core_scenes` schema; it emits IC notice
events on events.&lt;game_id&gt;.scene.&lt;scene_id&gt;.ic and audits lifecycle
operations to its plugin-owned audit table.

Authorization model: every mutating RPC trusts that the host&#39;s ABAC engine
has already authorized the command-execute action at dispatch time
(owner-only for end/pause/resume/update/invite/kick/transfer, admin-only for
the publish-attempt-budget extension). The plugin itself runs NO ABAC engine
(SceneServiceImpl holds no policy engine). The sole exceptions are the
participant-gate reads (GetPoseOrder and the publish reads), which enforce a
direct plugin-code participation check (INV-SCENE-60) precisely because it is a
hard privacy boundary that must not be delegable.

Implemented by SceneServiceImpl in plugins/core-scenes/service.go and
plugins/core-scenes/publish_service.go.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListScenes | [ListScenesRequest](#holomush-scene-v1-ListScenesRequest) | [ListScenesResponse](#holomush-scene-v1-ListScenesResponse) | ListScenes returns the public scene board: open scenes in state `active` or `paused`, paginated and optionally tag-filtered, with content-warning- blocked scenes excluded via the union of game/player/character scope cw_block settings plus any per-query exclude_content_warnings. Served by SceneServiceImpl.ListScenes and surfaced through the plugin&#39;s `scenes` board command. See service.go::ListScenes. |
| GetScene | [GetSceneRequest](#holomush-scene-v1-GetSceneRequest) | [GetSceneResponse](#holomush-scene-v1-GetSceneResponse) | GetScene loads one scene&#39;s metadata and participant roster by ID. The host&#39;s read-scene ABAC policy gates access before the call reaches the handler, so the handler performs no additional ownership check; it returns codes.NotFound when the scene does not exist. See service.go::GetScene. |
| CreateScene | [CreateSceneRequest](#holomush-scene-v1-CreateSceneRequest) | [CreateSceneResponse](#holomush-scene-v1-CreateSceneResponse) | CreateScene allocates a new scene owned by the calling character, seeded active with the supplied title/description/visibility/pose-order/tags. The creating character becomes the `owner` participant in the same transaction, a lifecycle.created audit event is recorded, and a scene-created event is emitted. See service.go::CreateScene. |
| EndScene | [EndSceneRequest](#holomush-scene-v1-EndSceneRequest) | [EndSceneResponse](#holomush-scene-v1-EndSceneResponse) | EndScene transitions a scene to the terminal `ended` state (owner-only via ABAC). Rejected with codes.FailedPrecondition when the scene is already ended or archived. Returns the post-transition scene row. See service.go::EndScene. |
| PauseScene | [PauseSceneRequest](#holomush-scene-v1-PauseSceneRequest) | [PauseSceneResponse](#holomush-scene-v1-PauseSceneResponse) | PauseScene transitions an `active` scene to `paused` (owner-only via ABAC). Rejected with codes.FailedPrecondition from any non-active state. See service.go::PauseScene. |
| ResumeScene | [ResumeSceneRequest](#holomush-scene-v1-ResumeSceneRequest) | [ResumeSceneResponse](#holomush-scene-v1-ResumeSceneResponse) | ResumeScene transitions a `paused` scene back to `active` (owner-only via ABAC). Rejected with codes.FailedPrecondition from any non-paused state. See service.go::ResumeScene. |
| UpdateScene | [UpdateSceneRequest](#holomush-scene-v1-UpdateSceneRequest) | [UpdateSceneResponse](#holomush-scene-v1-UpdateSceneResponse) | UpdateScene applies a partial update to mutable scene metadata, driven by the request&#39;s FieldMask (owner-only via ABAC). An empty mask is a no-op success. A pose-order-mode change auto-emits a pose-order-changed IC notice. See service.go::UpdateScene. |
| JoinScene | [JoinSceneRequest](#holomush-scene-v1-JoinSceneRequest) | [JoinSceneResponse](#holomush-scene-v1-JoinSceneResponse) | JoinScene adds the calling character to a scene as a `member`. Open scenes accept any join; private scenes require a pre-existing invitation (the invited row is promoted to member). Idempotent: a repeat join by an existing member succeeds without re-emitting a join notice. See service.go::JoinScene. |
| WatchScene | [WatchSceneRequest](#holomush-scene-v1-WatchSceneRequest) | [WatchSceneResponse](#holomush-scene-v1-WatchSceneResponse) | WatchScene auto-joins the requesting character into an OPEN scene as a role=observer participant and registers the focus membership for the supplied session, so focus/Subscribe/history gates admit the watcher. Gate order is fail-closed per INV-SCENE-61: the plugin-code visibility==open and state checks run BEFORE the ABAC spectate action is evaluated; non-open scenes are rejected without consulting ABAC. See service.go::WatchScene. |
| LeaveScene | [LeaveSceneRequest](#holomush-scene-v1-LeaveSceneRequest) | [LeaveSceneResponse](#holomush-scene-v1-LeaveSceneResponse) | LeaveScene removes the calling character from a scene. The scene owner cannot leave (codes.FailedPrecondition) — they must end the scene or transfer ownership first. Emits a leave IC notice with reason=left. See service.go::LeaveScene. |
| InviteToScene | [InviteToSceneRequest](#holomush-scene-v1-InviteToSceneRequest) | [InviteToSceneResponse](#holomush-scene-v1-InviteToSceneResponse) | InviteToScene records an `invited` participant row for a target character (owner-only via ABAC), granting them permission to join a private scene. Rejected with codes.AlreadyExists when the target is already a member. See service.go::InviteToScene. |
| KickFromScene | [KickFromSceneRequest](#holomush-scene-v1-KickFromSceneRequest) | [KickFromSceneResponse](#holomush-scene-v1-KickFromSceneResponse) | KickFromScene removes a target character from a scene (owner-only via ABAC). The scene owner cannot be kicked (codes.FailedPrecondition, enforced both at the service layer and by a store WHERE filter). Emits a leave IC notice with reason=kicked. See service.go::KickFromScene. |
| TransferOwnership | [TransferOwnershipRequest](#holomush-scene-v1-TransferOwnershipRequest) | [TransferOwnershipResponse](#holomush-scene-v1-TransferOwnershipResponse) | TransferOwnership reassigns scene ownership from the calling owner to a target who MUST already be a member (owner-only via ABAC). The former owner is demoted to member. See service.go::TransferOwnership. |
| CastPublishVote | [CastPublishVoteRequest](#holomush-scene-v1-CastPublishVoteRequest) | [CastPublishVoteResponse](#holomush-scene-v1-CastPublishVoteResponse) | CastPublishVote is DECLARED BUT NOT SERVED. It is the legacy scene-keyed publish-vote shape, superseded by CastPublishSceneVote (which is keyed by published_scene_id and is the served vote RPC). The plugin provides no handler, so a call returns codes.Unimplemented. |
| GetPoseOrder | [GetPoseOrderRequest](#holomush-scene-v1-GetPoseOrderRequest) | [GetPoseOrderResponse](#holomush-scene-v1-GetPoseOrderResponse) | GetPoseOrder returns the computed pose-order roster for a scene. Enforces the INV-SCENE-60 plugin-code participant gate (caller MUST be an owner or member, NOT merely invited; NO ABAC engine is consulted). The PermissionDenied gate fires before any existence check so a non-participant cannot distinguish a missing scene from one they may not see. See service.go::GetPoseOrder. |
| StartScenePublish | [StartScenePublishRequest](#holomush-scene-v1-StartScenePublishRequest) | [StartScenePublishResponse](#holomush-scene-v1-StartScenePublishResponse) | StartScenePublish opens a publication attempt for an `ended` scene (publish.go §5 precondition ladder). The scene must be ended, must not already have a published archive (one-and-done) nor an active attempt, and must not have exhausted its attempt budget. Seeds a COLLECTING attempt with a frozen vote roster. See publish_service.go::StartScenePublish. |
| CastPublishSceneVote | [CastPublishSceneVoteRequest](#holomush-scene-v1-CastPublishSceneVoteRequest) | [CastPublishSceneVoteResponse](#holomush-scene-v1-CastPublishSceneVoteResponse) | CastPublishSceneVote records a roster member&#39;s yes/no vote on an active publication attempt and runs the §4.3 resolution check, which may transition the attempt (COLLECTING→COOLOFF on all-yes, COLLECTING→ ATTEMPT_FAILED on any-no-after-all-voted, or COOLOFF→COLLECTING on a flip to no). A vote on a terminal attempt is rejected. The recorded vote is the durable effect; a failed resolution or emit is logged but does not fail the cast. See publish_service.go::CastPublishSceneVote. |
| WithdrawScenePublish | [WithdrawScenePublishRequest](#holomush-scene-v1-WithdrawScenePublishRequest) | [WithdrawScenePublishResponse](#holomush-scene-v1-WithdrawScenePublishResponse) | WithdrawScenePublish lets the scene owner abandon an active publication attempt (COLLECTING or COOLOFF), transitioning it to ATTEMPT_FAILED with failure_reason WITHDRAWN. Owner-gated by ABAC AND a defense-in-depth in-handler owner check (the plugin holds the owner attribute, so this closes the direct-RPC gap). See publish_service.go::WithdrawScenePublish. |
| GetPublishedScene | [GetPublishedSceneRequest](#holomush-scene-v1-GetPublishedSceneRequest) | [GetPublishedSceneResponse](#holomush-scene-v1-GetPublishedSceneResponse) | GetPublishedScene returns a publication attempt&#39;s full state to a scene participant. Enforces the INV-SCENE-60 plugin-code participant gate with a load-bearing step order (INV-SCENE-32): header read → participant gate → content read (only for PUBLISHED rows, only after the gate passes). A non-participant is denied with the §10 triple-signal before any content is read. See publish_service.go::GetPublishedScene. |
| DownloadPublishedScene | [DownloadPublishedSceneRequest](#holomush-scene-v1-DownloadPublishedSceneRequest) | [DownloadPublishedSceneResponse](#holomush-scene-v1-DownloadPublishedSceneResponse) | DownloadPublishedScene returns a PUBLISHED attempt rendered in the requested format (markdown/plain_text/jsonl) to a participant. Same load-bearing participant-gate ordering as GetPublishedScene; only PUBLISHED attempts are downloadable. See publish_service.go::DownloadPublishedScene. |
| ListScenePublishAttempts | [ListScenePublishAttemptsRequest](#holomush-scene-v1-ListScenePublishAttemptsRequest) | [ListScenePublishAttemptsResponse](#holomush-scene-v1-ListScenePublishAttemptsResponse) | ListScenePublishAttempts returns the audit list of a scene&#39;s publication attempts (header summaries, no content) to a participant. Participant-gated (INV-SCENE-60) so a non-participant cannot enumerate attempts. See publish_service.go::ListScenePublishAttempts. |
| GetPublicSceneArchive | [GetPublicSceneArchiveRequest](#holomush-scene-v1-GetPublicSceneArchiveRequest) | [GetPublicSceneArchiveResponse](#holomush-scene-v1-GetPublicSceneArchiveResponse) | GetPublishedScene&#39;s PUBLIC counterpart: GetPublicSceneArchive is the unauthenticated read of a published scene. Structurally separate — NO caller validation, NO participant gate, NO ABAC. The only gate is status==PUBLISHED; a missing id OR any non-PUBLISHED attempt returns one opaque NOT_FOUND so existence/progress of an attempt cannot be inferred (INV-SCENE-35). Carries only public-safe fields. See publish_service.go::GetPublicSceneArchive. |
| DownloadPublicSceneArchive | [DownloadPublicSceneArchiveRequest](#holomush-scene-v1-DownloadPublicSceneArchiveRequest) | [DownloadPublicSceneArchiveResponse](#holomush-scene-v1-DownloadPublicSceneArchiveResponse) | DownloadPublicSceneArchive is the PUBLIC, unauthenticated download of a published scene in the requested format. Same status-gate and opacity contract (INV-SCENE-35) as GetPublicSceneArchive; shares the renderer with DownloadPublishedScene. See publish_service.go::DownloadPublicSceneArchive. |
| ExtendScenePublishVoteAttempts | [ExtendScenePublishVoteAttemptsRequest](#holomush-scene-v1-ExtendScenePublishVoteAttemptsRequest) | [ExtendScenePublishVoteAttemptsResponse](#holomush-scene-v1-ExtendScenePublishVoteAttemptsResponse) | ExtendScenePublishVoteAttempts raises a scene&#39;s max-publish-attempts budget by a positive amount and emits the extension notice. Admin-only, enforced by the host&#39;s ABAC policy at dispatch — there is deliberately NO in-plugin role check (the inverse of INV-SCENE-60&#39;s plugin-code privacy gate). See publish_service.go::ExtendScenePublishVoteAttempts. |
| ListCharacterScenes | [ListCharacterScenesRequest](#holomush-scene-v1-ListCharacterScenesRequest) | [ListCharacterScenesResponse](#holomush-scene-v1-ListCharacterScenesResponse) | ListCharacterScenes returns every non-archived scene the character has a participant row in (any role, including observer), with the character&#39;s role and per-scene activity metadata for workspace badges. Serves the web workspace&#39;s &#34;my scenes&#34; list; intended for use by the host facade fanning this out across a player&#39;s owned characters. See service.go::ListCharacterScenes. |
| ListPublishedScenes | [ListPublishedScenesRequest](#holomush-scene-v1-ListPublishedScenesRequest) | [ListPublishedScenesResponse](#holomush-scene-v1-ListPublishedScenesResponse) | ListPublishedScenes pages through PUBLISHED scene archives (public-safe fields only, same status gate as GetPublicSceneArchive / INV-SCENE-35), newest first, with optional tag filtering. Powers the archive browse page. See publish_service.go::ListPublishedScenes. |
| ExportSceneLog | [ExportSceneLogRequest](#holomush-scene-v1-ExportSceneLogRequest) | [ExportSceneLogResponse](#holomush-scene-v1-ExportSceneLogResponse) | ExportSceneLog renders a scene&#39;s IC log to a downloadable document for a participant of ANY role (observers may export what they may read; INV-SCENE-60&#39;s participant gate is plugin-code-enforced — non-participants fail before ABAC, which is never consulted here). Decryption flows through the host-mediated snapshot decrypt seam; supported formats are &#34;markdown&#34; and &#34;jsonl&#34;. Scenes whose IC log exceeds the server-side row ceiling (exportLogMaxRows = 10 000) return FAILED_PRECONDITION / SCENE_EXPORT_TOO_LARGE rather than silently truncating the document. See export.go::ExportSceneLog. |

 



<a name="holomush_sceneaccess_v1_sceneaccess-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/sceneaccess/v1/sceneaccess.proto



<a name="holomush-sceneaccess-v1-DownloadPublicSceneArchiveRequest"></a>

### DownloadPublicSceneArchiveRequest
DownloadPublicSceneArchiveRequest is the facade request for downloading a
PUBLISHED scene archive without participant authentication. session_id and
player_session_token authenticate the calling player; guest players are
rejected (INV-SCENE-64). Same opacity contract as GetPublicSceneArchiveRequest
(INV-SCENE-35).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. Guest players are denied with codes.PermissionDenied (INV-SCENE-64). |
| published_scene_id | [string](#string) |  | published_scene_id identifies the publication attempt to download; required. Same opacity contract as GetPublicSceneArchiveRequest (INV-SCENE-35). |
| format | [string](#string) |  | format is the render format; required. Supported: &#34;markdown&#34;, &#34;plain_text&#34;, &#34;jsonl&#34;. |






<a name="holomush-sceneaccess-v1-DownloadPublicSceneArchiveResponse"></a>

### DownloadPublicSceneArchiveResponse
DownloadPublicSceneArchiveResponse wraps the plugin&#39;s rendered public-archive
bytes and their MIME type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | content is the rendered file content. |
| mime_type | [string](#string) |  | mime_type is the content&#39;s MIME type (text/markdown, text/plain, or application/jsonl). |






<a name="holomush-sceneaccess-v1-ExportSceneRequest"></a>

### ExportSceneRequest
ExportSceneRequest is the facade request for scene IC log export.
session_id and player_session_token authenticate the calling player; the
facade resolves the acting character SERVER-SIDE and ignores/overrides any
client-supplied identity (INV-SCENE-63).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. The client-supplied character_id is then verified as owned by this session&#39;s player (INV-SCENE-63). |
| character_id | [string](#string) |  | character_id is the &#34;act as this alt&#34; selector. The facade verifies ownership against the authenticated player before passing the server-verified ID downstream; a character not owned by the player returns codes.NotFound (INV-SCENE-63). |
| scene_id | [string](#string) |  | scene_id identifies the scene to export; required. |
| format | [string](#string) |  | format is the render format; required. Supported: &#34;markdown&#34; or &#34;jsonl&#34;. |






<a name="holomush-sceneaccess-v1-ExportSceneResponse"></a>

### ExportSceneResponse
ExportSceneResponse wraps the plugin&#39;s rendered scene-log document.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | content is the rendered document bytes. |
| mime_type | [string](#string) |  | mime_type is the content&#39;s MIME type (text/markdown or application/jsonl). |
| filename | [string](#string) |  | filename is the suggested download filename (slugified title &#43; extension). |






<a name="holomush-sceneaccess-v1-GetPublicSceneArchiveRequest"></a>

### GetPublicSceneArchiveRequest
GetPublicSceneArchiveRequest is the facade request for reading a single
published scene archive without participant authentication. session_id and
player_session_token authenticate the calling player; guest players are
rejected (INV-SCENE-64). The only gate on the plugin call is
status==PUBLISHED (INV-SCENE-35).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. Guest players are denied with codes.PermissionDenied (INV-SCENE-64). |
| published_scene_id | [string](#string) |  | published_scene_id identifies the publication attempt to read; required. A missing id or any non-PUBLISHED attempt returns one opaque NOT_FOUND (INV-SCENE-35). |






<a name="holomush-sceneaccess-v1-GetPublicSceneArchiveResponse"></a>

### GetPublicSceneArchiveResponse
GetPublicSceneArchiveResponse wraps the plugin&#39;s public archive view.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the publication attempt&#39;s ID. |
| title_snapshot | [string](#string) |  | title_snapshot is the scene title snapshotted at publish time. |
| participants_snapshot | [string](#string) | repeated | participants_snapshot is the participant character names snapshotted at publish time. |
| content_entries | [holomush.scene.v1.PublishedSceneEntry](#holomush-scene-v1-PublishedSceneEntry) | repeated | content_entries is the frozen published content. |
| published_at_unix_ns | [int64](#int64) |  | published_at_unix_ns is the epoch-nanosecond publish time. |






<a name="holomush-sceneaccess-v1-GetSceneForViewerRequest"></a>

### GetSceneForViewerRequest
GetSceneForViewerRequest is the facade request for loading one scene.
session_id and player_session_token authenticate the calling player; the
facade resolves the acting character SERVER-SIDE and ignores/overrides any
client-supplied identity (INV-SCENE-63).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. The client-supplied character_id is then verified as owned by this session&#39;s player (INV-SCENE-63). |
| character_id | [string](#string) |  | character_id is the &#34;act as this alt&#34; selector. The facade verifies ownership against the authenticated player before passing the server-verified ID downstream; a character not owned by the player returns codes.NotFound (INV-SCENE-63). |
| scene_id | [string](#string) |  | scene_id identifies the scene to load; required. |






<a name="holomush-sceneaccess-v1-GetSceneForViewerResponse"></a>

### GetSceneForViewerResponse
GetSceneForViewerResponse wraps the plugin&#39;s single-scene response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scene | [holomush.scene.v1.SceneInfo](#holomush-scene-v1-SceneInfo) |  | scene is the loaded scene&#39;s full metadata projection. |






<a name="holomush-sceneaccess-v1-ListMyScenesRequest"></a>

### ListMyScenesRequest
ListMyScenesRequest is the facade request for a character&#39;s scene
participations. session_id and player_session_token authenticate the calling
player; the facade resolves the acting character SERVER-SIDE and
ignores/overrides any client-supplied identity (INV-SCENE-63).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. The client-supplied character_id is then verified as owned by this session&#39;s player (INV-SCENE-63). |
| character_id | [string](#string) |  | character_id is the &#34;act as this alt&#34; selector. The facade verifies ownership against the authenticated player before passing the server-verified ID downstream; a character not owned by the player returns codes.NotFound (INV-SCENE-63). |






<a name="holomush-sceneaccess-v1-ListMyScenesResponse"></a>

### ListMyScenesResponse
ListMyScenesResponse wraps the plugin&#39;s character-scene participations list.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scenes | [holomush.scene.v1.CharacterSceneInfo](#holomush-scene-v1-CharacterSceneInfo) | repeated | scenes is the character&#39;s scene participations, most recently active first. |






<a name="holomush-sceneaccess-v1-ListPublishedScenesRequest"></a>

### ListPublishedScenesRequest
ListPublishedScenesRequest is the facade request to page through publicly
visible PUBLISHED scene archives. session_id and player_session_token
authenticate the calling player; guest players are rejected (INV-SCENE-64).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. Guest players are denied with codes.PermissionDenied (INV-SCENE-64). |
| limit | [int32](#int32) |  | limit is the page size; 0 means server default, capped at 200. |
| offset | [int32](#int32) |  | offset is the number of leading results to skip. |
| tags | [string](#string) | repeated | tags restricts results to archives whose source scene carries all of these tags. |






<a name="holomush-sceneaccess-v1-ListPublishedScenesResponse"></a>

### ListPublishedScenesResponse
ListPublishedScenesResponse wraps the plugin&#39;s public archive list.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| archives | [holomush.scene.v1.PublicSceneArchive](#holomush-scene-v1-PublicSceneArchive) | repeated | archives is the public-safe published-archive summaries, newest first. |






<a name="holomush-sceneaccess-v1-ListScenesForViewerRequest"></a>

### ListScenesForViewerRequest
ListScenesForViewerRequest is the facade request for the public scene board.
session_id and player_session_token authenticate the calling player; the
facade resolves the acting character SERVER-SIDE and ignores/overrides any
client-supplied identity (INV-SCENE-63).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. The client-supplied character_id is then verified as owned by this session&#39;s player (INV-SCENE-63). |
| character_id | [string](#string) |  | character_id is the &#34;act as this alt&#34; selector. The facade verifies ownership against the authenticated player before passing the server-verified ID downstream; a character not owned by the player returns codes.NotFound (INV-SCENE-63). |
| limit | [int32](#int32) |  | limit is the maximum number of scenes to return; 0 means server default, capped at 200 by the plugin. |
| offset | [int32](#int32) |  | offset is the number of leading results to skip for pagination. |
| tags | [string](#string) | repeated | tags restricts results to scenes carrying all of these tags. |
| exclude_content_warnings | [string](#string) | repeated | exclude_content_warnings adds extra content-warning categories to hide for this query, on top of the player&#39;s and character&#39;s stored block sets. |






<a name="holomush-sceneaccess-v1-ListScenesForViewerResponse"></a>

### ListScenesForViewerResponse
ListScenesForViewerResponse wraps the plugin&#39;s scene-board result page.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scenes | [holomush.scene.v1.SceneInfo](#holomush-scene-v1-SceneInfo) | repeated | scenes is the matching public scenes for this page. |






<a name="holomush-sceneaccess-v1-SetSceneFocusRequest"></a>

### SetSceneFocusRequest
SetSceneFocusRequest is the facade request to set per-connection scene focus
for a web portal connection. session_id and player_session_token authenticate
the calling player; the facade verifies that the connection belongs to a game
session owned by one of the player&#39;s characters before calling
SetConnectionFocus (INV-SCENE-63).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated (INV-SCENE-63). |
| connection_id | [string](#string) |  | connection_id is the ULID of the web portal connection whose focus to set; required. The facade verifies the connection belongs to a game session owned by one of this player&#39;s characters. |
| scene_id | [string](#string) |  | scene_id is the scene to focus on; optional. When empty, the focus is cleared (grid default). |






<a name="holomush-sceneaccess-v1-SetSceneFocusResponse"></a>

### SetSceneFocusResponse
SetSceneFocusResponse is intentionally empty — a successful focus set
carries no body; the client may update its local focus state optimistically.






<a name="holomush-sceneaccess-v1-WatchSceneRequest"></a>

### WatchSceneRequest
WatchSceneRequest is the facade request for observer auto-join. session_id
and player_session_token authenticate the calling player; the facade
resolves the acting character SERVER-SIDE and ignores/overrides any
client-supplied identity (INV-SCENE-63). The character&#39;s existing game
session is looked up server-side via FindByCharacter; the caller MUST have
selected the character (via SelectCharacter) before calling WatchScene.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the player session — used together with player_session_token to authenticate the calling player. |
| player_session_token | [string](#string) |  | player_session_token is the raw bearer token for the player session. The facade looks up the session by token hash and rejects unauthenticated callers with codes.Unauthenticated. The client-supplied character_id is then verified as owned by this session&#39;s player (INV-SCENE-63). |
| character_id | [string](#string) |  | character_id is the &#34;act as this alt&#34; selector. The facade verifies ownership against the authenticated player before passing the server-verified ID downstream; a character not owned by the player returns codes.NotFound (INV-SCENE-63). |
| scene_id | [string](#string) |  | scene_id identifies the scene to watch; required. Must be visibility=open and active or paused. |






<a name="holomush-sceneaccess-v1-WatchSceneResponse"></a>

### WatchSceneResponse
WatchSceneResponse wraps the plugin&#39;s observer-join confirmation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| participant | [holomush.scene.v1.ParticipantInfo](#holomush-scene-v1-ParticipantInfo) |  | participant is the resulting participant entry (role=observer), or the pre-existing row when the character was already a participant. |





 

 

 


<a name="holomush-sceneaccess-v1-SceneAccessService"></a>

### SceneAccessService
SceneAccessService is the host-side facade that owns player authentication,
identity resolution, and guest-player rejection for all scene-surface RPCs
exposed through the web portal. It wraps the plugin SceneService, ensuring
that every downstream call carries a server-verified, player-owned character
identity rather than a client-supplied one (INV-SCENE-63). Guests are denied
at this layer before any plugin call is attempted (INV-SCENE-64).

Implemented by SceneAccessServer in internal/grpc/sceneaccess_service.go.
Registered on the core gRPC server (not the plugin proxy) in
cmd/holomush/sub_grpc.go.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListScenesForViewer | [ListScenesForViewerRequest](#holomush-sceneaccess-v1-ListScenesForViewerRequest) | [ListScenesForViewerResponse](#holomush-sceneaccess-v1-ListScenesForViewerResponse) | ListScenesForViewer returns the public scene board filtered by the verified player&#39;s character-scope and player-scope content-warning blocks. The facade resolves the acting character from the player session (INV-SCENE-63) and forwards a ListScenes call to the plugin SceneService with the server-verified character_id. |
| GetSceneForViewer | [GetSceneForViewerRequest](#holomush-sceneaccess-v1-GetSceneForViewerRequest) | [GetSceneForViewerResponse](#holomush-sceneaccess-v1-GetSceneForViewerResponse) | GetSceneForViewer loads one scene&#39;s metadata for the verified player&#39;s owned character. The facade resolves the acting character from the player session (INV-SCENE-63) and forwards a GetScene call to the plugin SceneService with the server-verified character_id. |
| ListMyScenes | [ListMyScenesRequest](#holomush-sceneaccess-v1-ListMyScenesRequest) | [ListMyScenesResponse](#holomush-sceneaccess-v1-ListMyScenesResponse) | ListMyScenes returns every non-archived scene the verified player&#39;s owned character participates in (any role), with activity metadata for workspace badge rendering. The facade resolves the acting character from the player session (INV-SCENE-63) and forwards a ListCharacterScenes call to the plugin SceneService with the server-verified character_id. |
| WatchScene | [WatchSceneRequest](#holomush-sceneaccess-v1-WatchSceneRequest) | [WatchSceneResponse](#holomush-sceneaccess-v1-WatchSceneResponse) | WatchScene auto-joins the verified player&#39;s owned character into an OPEN, active scene as a role=observer participant and registers a FocusMembership on the character&#39;s existing game session (which must already exist — use SelectCharacter first). The facade resolves the acting character from the player session (INV-SCENE-63), looks up the character&#39;s game session via FindByCharacter, and forwards a WatchScene call to the plugin SceneService with the server-verified character_id and session_id. Returns FailedPrecondition when no game session exists for the character (select the character first). |
| ExportScene | [ExportSceneRequest](#holomush-sceneaccess-v1-ExportSceneRequest) | [ExportSceneResponse](#holomush-sceneaccess-v1-ExportSceneResponse) | ExportScene renders the verified player&#39;s owned character&#39;s scene IC log to a downloadable document. The facade resolves the acting character from the player session (INV-SCENE-63) and forwards an ExportSceneLog call to the plugin SceneService with the server-verified character_id. |
| SetSceneFocus | [SetSceneFocusRequest](#holomush-sceneaccess-v1-SetSceneFocusRequest) | [SetSceneFocusResponse](#holomush-sceneaccess-v1-SetSceneFocusResponse) | SetSceneFocus sets the per-connection focus for a web portal connection belonging to the verified player&#39;s character. The facade verifies that the connection belongs to a session owned by one of the player&#39;s characters (INV-SCENE-63) before calling the focus coordinator&#39;s SetConnectionFocus. |
| ListPublishedScenes | [ListPublishedScenesRequest](#holomush-sceneaccess-v1-ListPublishedScenesRequest) | [ListPublishedScenesResponse](#holomush-sceneaccess-v1-ListPublishedScenesResponse) | ListPublishedScenes pages through publicly visible PUBLISHED scene archives, newest first, with optional tag filtering. No character identity is required for the underlying query, but the player session token &#43; guest gate are still enforced (INV-SCENE-64). The facade forwards a ListPublishedScenes call to the plugin SceneService. |
| GetPublicSceneArchive | [GetPublicSceneArchiveRequest](#holomush-sceneaccess-v1-GetPublicSceneArchiveRequest) | [GetPublicSceneArchiveResponse](#holomush-sceneaccess-v1-GetPublicSceneArchiveResponse) | GetPublicSceneArchive reads a published scene archive without participant authentication. The only gate on the plugin call is status==PUBLISHED (INV-SCENE-35); the facade adds the player-session token check and guest-player denial (INV-SCENE-64). Forwards a GetPublicSceneArchive call to the plugin SceneService. |
| DownloadPublicSceneArchive | [DownloadPublicSceneArchiveRequest](#holomush-sceneaccess-v1-DownloadPublicSceneArchiveRequest) | [DownloadPublicSceneArchiveResponse](#holomush-sceneaccess-v1-DownloadPublicSceneArchiveResponse) | DownloadPublicSceneArchive returns a PUBLISHED scene archive rendered in the requested format. Same status-gate and opacity contract as GetPublicSceneArchive (INV-SCENE-35); the facade enforces the player-session token check and guest-player denial (INV-SCENE-64). Forwards a DownloadPublicSceneArchive call to the plugin SceneService. |

 



<a name="holomush_web_v1_web-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/web/v1/web.proto



<a name="holomush-web-v1-CharacterSummary"></a>

### CharacterSummary
CharacterSummary is the web-facing roster row for one of a player&#39;s
characters, mirroring corev1.CharacterSummary and used across the auth and
character-management responses.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the character&#39;s ULID identity. |
| character_name | [string](#string) |  | character_name is the character&#39;s display name. |
| has_active_session | [bool](#bool) |  | has_active_session is true when the character currently has a live game session. |
| session_status | [string](#string) |  | session_status is a human-readable status string for any active session (e.g. attached/detached state). |
| last_location | [string](#string) |  | last_location is the display label of the character&#39;s most recent location. |
| last_played_at | [int64](#int64) |  | last_played_at is the epoch-seconds timestamp of the character&#39;s most recent play activity. |






<a name="holomush-web-v1-ControlFrame"></a>

### ControlFrame
ControlFrame is the out-of-band stream-lifecycle message carried in the
`control` arm of StreamEventsResponse. It conveys open/replay/close
transitions and the per-stream routing identity, distinct from the in-band
GameEvent traffic.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-web-v1-ControlSignal) |  | signal selects which lifecycle transition this frame represents and governs which of the remaining fields are populated. |
| message | [string](#string) |  | message is an optional human-readable detail string accompanying the signal (e.g. a close reason). May be empty. |
| connection_id | [string](#string) |  | connection_id is populated on the first ControlFrame after a successful StreamEvents open so the client can include it in subsequent SendCommand requests. Per-stream identity for multi-tab routing (Phase 5 scene-focus autofocus). Empty on non-open frames. |
| attach_moment_ms | [int64](#int64) |  | attach_moment_ms is the server&#39;s wall-clock epoch-ms at the moment the Subscribe handler attached its durable consumer. Carried ONLY on CONTROL_SIGNAL_REPLAY_COMPLETE; clients reading other signals MUST ignore this field. The client passes this value as not_after_ms on subsequent backfill (WebQueryStreamHistory) calls so backfill returns ONLY events with timestamp &lt;= attach_moment_ms — eliminating the connect-time replay/backfill race where a post-attach event could appear both as a dimmed backfill row and a live Subscribe delivery (holomush-iu8j; fujt Fix B). 0 on legacy/pre-iu8j servers; clients MUST treat 0 as &#34;no upper bound&#34; (back-compat). |
| scene_id | [string](#string) |  | scene_id identifies the scene that produced a SCENE_ACTIVITY signal; the bare scene ULID (not a subject). Set ONLY on CONTROL_SIGNAL_SCENE_ACTIVITY; clients reading other signals MUST ignore it. |






<a name="holomush-web-v1-DisconnectRequest"></a>

### DisconnectRequest
DisconnectRequest names the session to tear down out of band.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the in-game session to end. |






<a name="holomush-web-v1-DisconnectResponse"></a>

### DisconnectResponse
DisconnectResponse is the empty acknowledgement of a Disconnect call; the
gateway always returns it even when the upstream best-effort RPC failed.






<a name="holomush-web-v1-GameEvent"></a>

### GameEvent
GameEvent is the web-facing rendering of a single core EventFrame, flattened
for direct display by the client. The gateway derives every field from the
core EventFrame and its RenderingMetadata in
internal/web/translate.go::translateEvent; events lacking rendering metadata
are dropped at the gateway (INV-EVENTBUS-6) and never reach this message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  | type is the event-type discriminator (e.g. &#34;say&#34;, &#34;pose&#34;, &#34;arrive&#34;, &#34;location_state&#34;), forwarded from corev1.EventFrame.type. |
| category | [string](#string) |  | category is the rendering category from RenderingMetadata (e.g. &#34;communication&#34;, &#34;movement&#34;, &#34;state&#34;); it drives client-side grouping and the gateway&#39;s own state-vs-narrative branch. |
| format | [string](#string) |  | format is the rendering format hint from RenderingMetadata (e.g. &#34;speech&#34;, &#34;action&#34;) guiding how the client styles the line. |
| display_target | [EventChannel](#holomush-web-v1-EventChannel) |  | display_target is the surface the event renders on (terminal, state, or both), copied from RenderingMetadata.display_target. |
| timestamp | [int64](#int64) |  | timestamp is the event time as epoch SECONDS, taken from the seconds component of corev1.EventFrame.timestamp. |
| actor | [string](#string) |  | actor is the DISPLAY NAME of the acting character, extracted from the event payload (character_name, falling back to sender_name) for rendering. For stable identity use actor_id, not this field. |
| text | [string](#string) |  | text is the rendered line shown to the player. Extracted from the payload (message → text → action → notice) or synthesized for arrive/leave movement events. Empty for state events whose content lives in metadata. |
| metadata | [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct) |  | metadata carries type-specific extras (label, style, channel, no_space, target_name) for narrative events, or the entire decoded payload for state-category events. |
| event_id | [string](#string) |  | event_id is the originating event&#39;s ULID, forwarded from corev1.EventFrame.id; the client uses it for dedup keying. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor for this event. Mirrors corev1.EventFrame.cursor for reconnect-with-backfill support. |
| actor_id | [string](#string) |  | actor_id is the ULID identity of the actor (character/plugin/system), forwarded from corev1.EventFrame.actor_id. Distinct from `actor` above which is the display name extracted from the JSON payload — name is for rendering; actor_id is for stable cross-event keying (e.g., presence list dedup, self-message detection, ABAC correlation). Empty for events without a typed actor. Added by holomush-5b2j.13. |






<a name="holomush-web-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest
GetCommandHistoryRequest names the session whose recent command lines to
retrieve.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the in-game session whose command history to fetch. |






<a name="holomush-web-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse
GetCommandHistoryResponse returns the session&#39;s recent command lines, or an
empty list when the lookup failed or was unauthorized.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [string](#string) | repeated | commands is the ordered list of recent raw command lines for the session. |






<a name="holomush-web-v1-SendCommandRequest"></a>

### SendCommandRequest
SendCommandRequest carries one raw command line for a game session, optionally
tagged with the stream connection it originated from.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the in-game session whose character issues the command. |
| text | [string](#string) |  | text is the raw command line exactly as the player typed it; parsing and dispatch happen core-side in HandleCommand. |
| connection_id | [string](#string) |  | connection_id identifies the originating StreamEvents stream for per-connection command routing (Phase 5 scene-focus autofocus). Clients set this from the connection_id they receive in the STREAM_OPENED ControlFrame after StreamEvents opens. Empty means &#34;no specific connection origin&#34; (scripted / admin paths). |






<a name="holomush-web-v1-SendCommandResponse"></a>

### SendCommandResponse
SendCommandResponse reports the outcome of a dispatched command. Note this is
the dispatch acknowledgement; any narrative output the command produces
arrives asynchronously over the StreamEvents feed, not here.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the command was accepted and dispatched without a transport-level error. |
| output | [string](#string) |  | output is an optional synchronous text result; most game output is delivered out of band over StreamEvents rather than in this field. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail; the gateway sets a generic &#34;command error&#34; when the upstream HandleCommand RPC fails. |






<a name="holomush-web-v1-StreamEventsRequest"></a>

### StreamEventsRequest
StreamEventsRequest opens the server-streaming event feed for a session.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the in-game session whose event stream to attach. |






<a name="holomush-web-v1-StreamEventsResponse"></a>

### StreamEventsResponse
StreamEventsResponse is one frame in the StreamEvents server stream: either
an in-band game event or an out-of-band control message, never both.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [GameEvent](#holomush-web-v1-GameEvent) |  | event is an in-band game event for display. |
| control | [ControlFrame](#holomush-web-v1-ControlFrame) |  | control is an out-of-band stream-lifecycle signal (open, replay boundary, close). |






<a name="holomush-web-v1-WebAuthenticatePlayerRequest"></a>

### WebAuthenticatePlayerRequest
WebAuthenticatePlayerRequest carries login credentials for the
WebAuthenticatePlayer RPC.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  | username identifies the player account to authenticate. |
| password | [string](#string) |  | password is the plaintext password verified core-side (never logged at the gateway). |
| remember_me | [bool](#bool) |  | remember_me requests a longer-lived session/cookie; forwarded to core to determine session TTL. |






<a name="holomush-web-v1-WebAuthenticatePlayerResponse"></a>

### WebAuthenticatePlayerResponse
WebAuthenticatePlayerResponse reports the login outcome and, on success, the
player&#39;s character roster. The session token itself is delivered as a
Set-Cookie header by the gateway, not in this body.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when credentials were accepted. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated | characters is the player&#39;s roster, returned so the client can prompt for character selection after login. |
| default_character_id | [string](#string) |  | default_character_id names the character the client should preselect, if the player has a default. |
| error_code | [string](#string) |  | error_code is a machine-readable error discriminator. Values: &#34;&#34; on success, &#34;ALREADY_AUTHENTICATED&#34; when the cookie-collision gate fires; others reserved for future use. |
| current_player_name | [string](#string) |  | current_player_name is populated only when error_code = &#34;ALREADY_AUTHENTICATED&#34;. Holds the existing player&#39;s display name so the client renders the right &#34;you are already signed in as X&#34; UI without a second round trip. |






<a name="holomush-web-v1-WebAvailableCommand"></a>

### WebAvailableCommand
WebAvailableCommand is one command&#39;s metadata for the web composer.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the canonical command name shown on the chip. |
| help | [string](#string) |  | help is the one-line description (for future tooltip use). |
| usage | [string](#string) |  | usage is the usage pattern. |
| source | [string](#string) |  | source is &#34;core&#34; or the owning plugin name. |






<a name="holomush-web-v1-WebCheckSessionRequest"></a>

### WebCheckSessionRequest
WebCheckSessionRequest is the empty request that validates the cookie-borne
player session; the token is read from the cookie header, not the body.






<a name="holomush-web-v1-WebCheckSessionResponse"></a>

### WebCheckSessionResponse
WebCheckSessionResponse returns the authenticated player&#39;s identity and
roster when the cookie session is valid. An invalid/expired session yields a
CodeUnauthenticated error instead of this message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  | player_name is the authenticated player&#39;s display name. |
| player_id | [string](#string) |  | player_id is the authenticated player&#39;s ULID identity. Additive on the success path; the failure path still returns CodeUnauthenticated so the web client&#39;s authed layout continues to redirect on throw (no contract break). |
| is_guest | [bool](#bool) |  | is_guest is true when the session belongs to an ephemeral guest player. |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated | characters is the player&#39;s character roster, returned so the client can restore character-selection state on reload. |






<a name="holomush-web-v1-WebConfirmPasswordResetRequest"></a>

### WebConfirmPasswordResetRequest
WebConfirmPasswordResetRequest carries the emailed reset token and the
replacement password.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | token is the single-use reset token delivered by email. |
| new_password | [string](#string) |  | new_password is the replacement plaintext password (hashed core-side). |






<a name="holomush-web-v1-WebConfirmPasswordResetResponse"></a>

### WebConfirmPasswordResetResponse
WebConfirmPasswordResetResponse reports whether the reset completed.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the password was changed. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |






<a name="holomush-web-v1-WebContentItem"></a>

### WebContentItem
WebContentItem is the web-facing representation of a single content-store
entry, forwarded field-for-field from contentv1.ContentItem.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | key is the item&#39;s storage key. |
| content_type | [string](#string) |  | content_type is the item&#39;s MIME type, guiding how the client renders the body. |
| body | [bytes](#bytes) |  | body is the raw item content bytes. |
| metadata | [WebContentItem.MetadataEntry](#holomush-web-v1-WebContentItem-MetadataEntry) | repeated | metadata is a free-form string→string map of item attributes. |






<a name="holomush-web-v1-WebContentItem-MetadataEntry"></a>

### WebContentItem.MetadataEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-web-v1-WebCreateCharacterRequest"></a>

### WebCreateCharacterRequest
WebCreateCharacterRequest names the new character to add to the authenticated
player. The player is identified by the cookie session token, not a body
field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_name | [string](#string) |  | character_name is the desired display name for the new character. Field number is 2 (field 1 retired with the cookie cutover). |






<a name="holomush-web-v1-WebCreateCharacterResponse"></a>

### WebCreateCharacterResponse
WebCreateCharacterResponse reports the result of creating a character.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the character was created. |
| character_id | [string](#string) |  | character_id is the new character&#39;s ULID identity. |
| character_name | [string](#string) |  | character_name is the created character&#39;s display name. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |






<a name="holomush-web-v1-WebCreateGuestRequest"></a>

### WebCreateGuestRequest
WebCreateGuestRequest is the empty request for provisioning an ephemeral
guest player; guest identity is generated server-side.






<a name="holomush-web-v1-WebCreateGuestResponse"></a>

### WebCreateGuestResponse
WebCreateGuestResponse reports the guest-provisioning outcome and the
generated guest character roster. The accompanying guest session cookie
(shorter TTL) is set via Set-Cookie by the gateway.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when a guest player and character were provisioned. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated | characters is the generated guest character roster. |
| default_character_id | [string](#string) |  | default_character_id names the guest character to preselect. |
| error_code | [string](#string) |  | error_code mirrors WebAuthenticatePlayerResponse.error_code semantics. |
| current_player_name | [string](#string) |  | current_player_name mirrors WebAuthenticatePlayerResponse.current_player_name. |






<a name="holomush-web-v1-WebCreatePlayerRequest"></a>

### WebCreatePlayerRequest
WebCreatePlayerRequest carries the fields for new-account registration.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  | username is the desired account name. |
| password | [string](#string) |  | password is the desired plaintext password (hashed core-side). |
| email | [string](#string) |  | email is the account&#39;s email address, used for password-reset delivery. |






<a name="holomush-web-v1-WebCreatePlayerResponse"></a>

### WebCreatePlayerResponse
WebCreatePlayerResponse reports the registration outcome and the initial
character roster. As with login, the session token is delivered via
Set-Cookie, not in this body.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the account was created. |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated | characters is the new account&#39;s initial character roster. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |
| error_code | [string](#string) |  | error_code mirrors WebAuthenticatePlayerResponse.error_code semantics (e.g. &#34;ALREADY_AUTHENTICATED&#34; from the cookie-collision gate). |
| current_player_name | [string](#string) |  | current_player_name mirrors WebAuthenticatePlayerResponse.current_player_name; set only on the ALREADY_AUTHENTICATED path. |






<a name="holomush-web-v1-WebGetContentRequest"></a>

### WebGetContentRequest
WebGetContentRequest selects one content-store item by exact key. Served by
the public, unauthenticated ContentService proxy (NOT CoreService).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | key is the exact content-store key; no prefix matching is applied. |






<a name="holomush-web-v1-WebGetContentResponse"></a>

### WebGetContentResponse
WebGetContentResponse returns the requested content item, or an empty message
when no item exists for the key.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| item | [WebContentItem](#holomush-web-v1-WebContentItem) |  | item is the matched content item; unset when the key was not found. |






<a name="holomush-web-v1-WebListCharactersRequest"></a>

### WebListCharactersRequest
WebListCharactersRequest is the empty request for the authenticated player&#39;s
roster; the player is identified by the cookie session token.






<a name="holomush-web-v1-WebListCharactersResponse"></a>

### WebListCharactersResponse
WebListCharactersResponse returns the authenticated player&#39;s character
roster.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated | characters is the player&#39;s full character roster. |






<a name="holomush-web-v1-WebListCommandsRequest"></a>

### WebListCommandsRequest
WebListCommandsRequest names the session whose character&#39;s commands to list.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the requesting session; core resolves the character and enforces ownership. |






<a name="holomush-web-v1-WebListCommandsResponse"></a>

### WebListCommandsResponse
WebListCommandsResponse returns the command set &#43; alias map for the composer.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [WebAvailableCommand](#holomush-web-v1-WebAvailableCommand) | repeated | commands is the set the session character may execute. |
| aliases | [WebListCommandsResponse.AliasesEntry](#holomush-web-v1-WebListCommandsResponse-AliasesEntry) | repeated | aliases maps alias → canonical command name for visible commands. |
| incomplete | [bool](#bool) |  | incomplete is true when engine errors hid some commands. |






<a name="holomush-web-v1-WebListCommandsResponse-AliasesEntry"></a>

### WebListCommandsResponse.AliasesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="holomush-web-v1-WebListContentRequest"></a>

### WebListContentRequest
WebListContentRequest selects a page of content-store items under a key
prefix. Served by the public ContentService proxy.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| prefix | [string](#string) |  | prefix restricts results to keys beginning with this string. |
| limit | [int32](#int32) |  | limit requests a page size; the bound is forwarded to and applied by the content service. |
| cursor | [string](#string) |  | cursor is the opaque next_cursor from a prior page; empty starts at the top of the prefix. |






<a name="holomush-web-v1-WebListContentResponse"></a>

### WebListContentResponse
WebListContentResponse returns one page of content items plus the cursor for
the following page.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [WebContentItem](#holomush-web-v1-WebContentItem) | repeated | items is the page of matched content items. |
| next_cursor | [string](#string) |  | next_cursor is the opaque cursor for the next page; empty when no further pages remain. |






<a name="holomush-web-v1-WebListFocusPresenceRequest"></a>

### WebListFocusPresenceRequest
WebListFocusPresenceRequest names the session whose current focus-context
presence to snapshot.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the requesting session; core resolves the session&#39;s current focus (location or scene) and enforces ownership. |






<a name="holomush-web-v1-WebListFocusPresenceResponse"></a>

### WebListFocusPresenceResponse
WebListFocusPresenceResponse returns the presence snapshot for the session&#39;s
current focus context.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| context | [WebPresenceContext](#holomush-web-v1-WebPresenceContext) |  | context indicates whether the snapshot describes a location or a scene. |
| context_id | [string](#string) |  | context_id is the ULID of the focus location or scene the snapshot describes. |
| entries | [WebPresenceEntry](#holomush-web-v1-WebPresenceEntry) | repeated | entries is the set of characters present in the focus context. |






<a name="holomush-web-v1-WebListPlayerSessionsRequest"></a>

### WebListPlayerSessionsRequest
WebListPlayerSessionsRequest is the empty request listing the caller&#39;s
PlayerSessions; the caller is identified by the cookie token.






<a name="holomush-web-v1-WebListPlayerSessionsResponse"></a>

### WebListPlayerSessionsResponse
WebListPlayerSessionsResponse returns all of the caller&#39;s active
PlayerSessions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [WebPlayerSessionInfo](#holomush-web-v1-WebPlayerSessionInfo) | repeated | sessions is the caller&#39;s list of active PlayerSessions, one flagged is_current. |






<a name="holomush-web-v1-WebListSessionStreamsRequest"></a>

### WebListSessionStreamsRequest
WebListSessionStreamsRequest names the session whose subscribed stream set to
enumerate.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the session whose subscriptions to list. |






<a name="holomush-web-v1-WebListSessionStreamsResponse"></a>

### WebListSessionStreamsResponse
WebListSessionStreamsResponse returns the stream names a session is subscribed
to, used by the client to drive reload-backfill across each stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| streams | [string](#string) | repeated | streams is the list of event-store stream names the session is subscribed to. Values are domain-relative dot references (e.g. &#34;location.&lt;id&gt;&#34;, &#34;character.&lt;id&gt;&#34;) — the same form the client passes back to WebQueryStreamHistory, which the server qualifies. |






<a name="holomush-web-v1-WebLogoutRequest"></a>

### WebLogoutRequest
WebLogoutRequest is the empty request to end the current session; the session
to end is identified by the cookie token.






<a name="holomush-web-v1-WebLogoutResponse"></a>

### WebLogoutResponse
WebLogoutResponse is the empty acknowledgement of logout. The meaningful
effect — clearing the session cookie — is delivered as a response header by
the gateway, so this body carries no fields.






<a name="holomush-web-v1-WebPlayerSessionInfo"></a>

### WebPlayerSessionInfo
WebPlayerSessionInfo describes one of a player&#39;s PlayerSessions — a
device/tab login record (distinct from an in-game game session) shown in the
account&#39;s &#34;active sessions&#34; management UI.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the PlayerSession.id (ULID). Used as target_session_id when revoking. Safe to show - resource handle, not a secret. |
| created_at | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | created_at is when the PlayerSession was first established (login time). |
| last_active | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  | last_active is the timestamp of the PlayerSession&#39;s most recent activity. |
| user_agent | [string](#string) |  | user_agent is the browser User-Agent recorded at login, shown to help the player recognize the device. |
| ip_address | [string](#string) |  | ip_address is the client IP recorded at login, shown for the same recognition purpose. |
| is_current | [bool](#bool) |  | is_current is true for the PlayerSession that made this request. |






<a name="holomush-web-v1-WebPresenceEntry"></a>

### WebPresenceEntry
WebPresenceEntry is one character in a presence snapshot, carrying identity
and activity state for the client&#39;s presence list.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the present character&#39;s ULID identity (used for dedup keying). |
| character_name | [string](#string) |  | character_name is the present character&#39;s display name. |
| state | [WebPresenceState](#holomush-web-v1-WebPresenceState) |  | state is the character&#39;s activity state in this context. |






<a name="holomush-web-v1-WebQueryStreamHistoryRequest"></a>

### WebQueryStreamHistoryRequest
WebQueryStreamHistoryRequest selects a page of historical events for a session
stream, with time-window and cursor bounds. Proxies to
CoreService.QueryStreamHistory.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  | session_id identifies the requesting session (used core-side for authorization). |
| stream | [string](#string) |  | stream names the event stream to read history from. Clients pass domain-relative dot-style references (e.g. &#34;location.&lt;id&gt;&#34;); the server qualifies them into fully-qualified JetStream subjects on the way in. |
| count | [int32](#int32) |  | count is the requested page size; 0 means the server default (150), values are capped at the server maximum (500), and negative values are rejected. |
| not_before_ms | [int64](#int64) |  | not_before_ms is the epoch-ms time floor; 0 means no lower bound. |
| cursor | [bytes](#bytes) |  | cursor is the opaque pagination cursor from a previous WebQueryStreamHistoryResponse. Events older than the cursor position are returned. Empty = start from latest. |
| not_after_ms | [int64](#int64) |  | not_after_ms is the epoch-ms time ceiling. 0 = no upper bound (back-compat). INCLUSIVE: events with timestamp == not_after_ms are returned. Set by the web client to the Subscribe attach_moment_ms (carried on the REPLAY_COMPLETE ControlFrame) so backfill returns only events that existed before the live stream attached — eliminating the connect-time replay/backfill race (holomush-iu8j; holomush-fujt Fix B). |






<a name="holomush-web-v1-WebQueryStreamHistoryResponse"></a>

### WebQueryStreamHistoryResponse
WebQueryStreamHistoryResponse returns one page of historical events translated
for the web client, plus pagination state.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [GameEvent](#holomush-web-v1-GameEvent) | repeated | events is the page of historical events (translated from core EventFrames; events without rendering metadata are dropped at the gateway). |
| has_more | [bool](#bool) |  | has_more is true when older events remain beyond this page. |
| next_cursor | [bytes](#bytes) |  | next_cursor is the opaque cursor for the next page. Empty if has_more is false. |






<a name="holomush-web-v1-WebRequestPasswordResetRequest"></a>

### WebRequestPasswordResetRequest
WebRequestPasswordResetRequest names the email address to begin a reset for.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  | email is the account email to send the reset link to. |






<a name="holomush-web-v1-WebRequestPasswordResetResponse"></a>

### WebRequestPasswordResetResponse
WebRequestPasswordResetResponse acknowledges the reset request. success is
reported true even when the underlying account does not exist, to avoid
leaking account existence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the request was accepted; it does NOT confirm the email exists (existence is deliberately not disclosed). |






<a name="holomush-web-v1-WebRevokeOtherPlayerSessionsRequest"></a>

### WebRevokeOtherPlayerSessionsRequest
WebRevokeOtherPlayerSessionsRequest is the empty &#34;log out everywhere else&#34;
request; the surviving (current) session is identified by the cookie token.






<a name="holomush-web-v1-WebRevokeOtherPlayerSessionsResponse"></a>

### WebRevokeOtherPlayerSessionsResponse
WebRevokeOtherPlayerSessionsResponse reports the bulk-revocation outcome and
how many PlayerSessions were revoked.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the bulk revocation completed. |
| revoked_count | [int32](#int32) |  | revoked_count is the number of other PlayerSessions revoked. |






<a name="holomush-web-v1-WebRevokePlayerSessionRequest"></a>

### WebRevokePlayerSessionRequest
WebRevokePlayerSessionRequest names a single PlayerSession to revoke; the
caller (and thus ownership) is identified by the cookie token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| target_session_id | [string](#string) |  | target_session_id is the PlayerSession.id (ULID) to revoke. |






<a name="holomush-web-v1-WebRevokePlayerSessionResponse"></a>

### WebRevokePlayerSessionResponse
WebRevokePlayerSessionResponse reports whether the targeted PlayerSession was
revoked.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when the targeted PlayerSession was revoked. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |






<a name="holomush-web-v1-WebSelectCharacterRequest"></a>

### WebSelectCharacterRequest
WebSelectCharacterRequest names the character to bind to a game session.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | character_id is the ULID of the character to select. Field number is 2; field 1 was retired with the cookie cutover (the token now travels in the cookie header, not the request body). |
| client_type | [string](#string) |  | client_type declares the surface establishing the session (terminal/comms_hub/telnet — the session_connections vocabulary). When &#34;comms_hub&#34;, a FRESH session creation skips the grid arrive emission: the web portal&#39;s scenes workspace must not announce the character on the grid (spec 2026-06-07 §V2). Empty preserves the legacy behavior (arrive). |






<a name="holomush-web-v1-WebSelectCharacterResponse"></a>

### WebSelectCharacterResponse
WebSelectCharacterResponse reports the result of binding a character,
including the resulting game session and whether an existing session was
reattached rather than freshly created.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | success is true when a session was created or reattached. |
| session_id | [string](#string) |  | session_id is the resulting in-game session&#39;s id, used by StreamEvents and SendCommand. |
| character_name | [string](#string) |  | character_name is the selected character&#39;s display name. |
| reattached | [bool](#bool) |  | reattached is true when an existing detached session was resumed instead of a new one being created. |
| error_message | [string](#string) |  | error_message is a human-readable failure detail on the non-success path. |





 


<a name="holomush-web-v1-ControlSignal"></a>

### ControlSignal
ControlSignal is the discriminator on a ControlFrame — an out-of-band
stream-lifecycle message multiplexed into the StreamEvents feed alongside
game events. The gateway forwards core control signals
(internal/web/handler.go::forwardFrame) and also synthesizes STREAM_OPENED
itself at attach time.

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 | CONTROL_SIGNAL_UNSPECIFIED is the zero value; a well-formed ControlFrame always carries a concrete signal, so this value SHOULD never appear on the wire. |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 | CONTROL_SIGNAL_REPLAY_COMPLETE marks the boundary between the historical backfill the core Subscribe handler replays at attach and the live event tail. The accompanying ControlFrame.attach_moment_ms is the upper time bound the client feeds back into WebQueryStreamHistory backfill calls. |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 | CONTROL_SIGNAL_STREAM_CLOSED signals the server is ending the stream; the gateway treats it as a terminal frame (forwardFrame returns errStreamClosed) and runs its session-cleanup Disconnect. |
| CONTROL_SIGNAL_STREAM_OPENED | 3 | STREAM_OPENED is emitted as the first frame after a successful StreamEvents subscription. The accompanying ControlFrame.connection_id is the per-stream ULID — clients SHOULD store it and pass it back via SendCommandRequest.connection_id so the gateway routes per-connection commands (Phase 5 scene-focus autofocus) correctly under multi-tab. |
| CONTROL_SIGNAL_RECONNECTING | 4 | CONTROL_SIGNAL_RECONNECTING: the gateway lost its core stream but is holding the client and reconnecting; the UI shows a reconnecting indicator (holomush-rsoe6). |
| CONTROL_SIGNAL_RECONNECTED | 5 | CONTROL_SIGNAL_RECONNECTED: the gateway re-established the core stream; the client may clear the reconnecting indicator. |
| CONTROL_SIGNAL_SCENE_ACTIVITY | 6 | CONTROL_SIGNAL_SCENE_ACTIVITY notifies the client that a scene it is a member of received an event while this connection was NOT focused on it. Carries scene_id only — no event content. Drives workspace unread badges; lossy by design (clients re-sync via ListMyScenes snapshots). |



<a name="holomush-web-v1-EventChannel"></a>

### EventChannel
EventChannel is the web mirror of corev1.EventChannel: it tells the web
client which surface a GameEvent should render on. The gateway forwards the
core RenderingMetadata.display_target verbatim onto GameEvent.display_target
(internal/web/translate.go::translateEvent), so the value semantics are
identical to the core enum — no remapping happens at the boundary.

| Name | Number | Description |
| ---- | ------ | ----------- |
| EVENT_CHANNEL_UNSPECIFIED | 0 | EVENT_CHANNEL_UNSPECIFIED is the zero value; rendering metadata that omits a display target lands here. The web client SHOULD treat it as a safe default rather than a routing instruction. |
| EVENT_CHANNEL_TERMINAL | 1 | EVENT_CHANNEL_TERMINAL routes the event to the scrolling terminal/log pane — the conversational stream (say, pose, arrive, leave, channel). |
| EVENT_CHANNEL_STATE | 2 | EVENT_CHANNEL_STATE routes the event to the structured state surfaces (location panel, exits, presence) rather than the terminal log. Used by state-category events such as location_state and exit_update. |
| EVENT_CHANNEL_BOTH | 3 | EVENT_CHANNEL_BOTH routes the event to the terminal AND the state surfaces simultaneously. |
| EVENT_CHANNEL_AUDIT_ONLY | 4 | EVENT_CHANNEL_AUDIT_ONLY mirrors corev1.EventChannel for INV-EVENTBUS-16 lockstep parity. These events are dropped at the gRPC Subscribe boundary and MUST NOT appear on the web wire format in practice. |



<a name="holomush-web-v1-WebPresenceContext"></a>

### WebPresenceContext
WebPresenceContext is the web mirror of corev1.PresenceContext: it tells the
client which kind of focus context a presence snapshot describes. The gateway
maps core values 1:1 (internal/web/handler.go::translatePresenceContext).

| Name | Number | Description |
| ---- | ------ | ----------- |
| WEB_PRESENCE_CONTEXT_UNSPECIFIED | 0 | WEB_PRESENCE_CONTEXT_UNSPECIFIED is the zero value / fallback for an unrecognized core context. |
| WEB_PRESENCE_CONTEXT_LOCATION | 1 | WEB_PRESENCE_CONTEXT_LOCATION indicates the snapshot is the presence at a location. |
| WEB_PRESENCE_CONTEXT_SCENE | 2 | WEB_PRESENCE_CONTEXT_SCENE indicates the snapshot is the participants of a scene. |



<a name="holomush-web-v1-WebPresenceState"></a>

### WebPresenceState
WebPresenceState is the web mirror of corev1.PresenceState: the activity
state of one present character. Mapped 1:1 from core
(internal/web/handler.go::translatePresenceState).

| Name | Number | Description |
| ---- | ------ | ----------- |
| WEB_PRESENCE_STATE_UNSPECIFIED | 0 | WEB_PRESENCE_STATE_UNSPECIFIED is the zero value / fallback for an unrecognized core state. |
| WEB_PRESENCE_STATE_ACTIVE | 1 | WEB_PRESENCE_STATE_ACTIVE means the character has a live, attached connection (grid present). |
| WEB_PRESENCE_STATE_DETACHED | 2 | WEB_PRESENCE_STATE_DETACHED means the session exists but currently has no attached connection. |
| WEB_PRESENCE_STATE_INACTIVE | 3 | WEB_PRESENCE_STATE_INACTIVE means the character is present but idle/not actively connected. |


 

 


<a name="holomush-web-v1-WebService"></a>

### WebService
WebService is the ConnectRPC surface the SvelteKit web client speaks to. Per
the gateway-boundary invariant (.claude/rules/gateway-boundary.md) it is a
protocol-translation layer ONLY: every game-state operation proxies to the
corresponding CoreService gRPC RPC (content RPCs proxy to ContentService),
and the gateway computes no business logic. The web-specific concerns it
owns are HTTP↔gRPC framing, cookie↔token translation
(internal/web/cookie.go::CookieMiddleware), and per-stream connection
identity. Implemented by internal/web.Handler; registered via
webv1connect.NewWebServiceHandler in internal/web/server.go.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SendCommand | [SendCommandRequest](#holomush-web-v1-SendCommandRequest) | [SendCommandResponse](#holomush-web-v1-SendCommandResponse) | SendCommand submits a player&#39;s raw command line (say, pose, quit, ...) for parsing and dispatch. Proxies to CoreService.HandleCommand; command-history persistence happens core-side, so the gateway does no extra work. The connection_id ties the command to its originating stream for per-connection routing. |
| StreamEvents | [StreamEventsRequest](#holomush-web-v1-StreamEventsRequest) | [StreamEventsResponse](#holomush-web-v1-StreamEventsResponse) stream | StreamEvents opens the server-streaming game-event feed for a session. Proxies to CoreService.Subscribe; the gateway generates a per-stream connection_id, emits a synthetic STREAM_OPENED ControlFrame carrying it, forwards translated frames, and runs a best-effort Disconnect on stream exit (client disconnect, cancel, error, or STREAM_CLOSED). Session-store registration/deregistration is owned entirely by core&#39;s Subscribe path. |
| Disconnect | [DisconnectRequest](#holomush-web-v1-DisconnectRequest) | [DisconnectResponse](#holomush-web-v1-DisconnectResponse) | Disconnect ends the game session out of band (not via stream teardown). Proxies to CoreService.Disconnect on a best-effort basis — RPC errors are logged but never surfaced to the caller. Forwards the session-token cookie header so core can enforce ownership. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-web-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-web-v1-GetCommandHistoryResponse) | GetCommandHistory retrieves the recent command lines a session has entered. Proxies to CoreService.GetCommandHistory; ownership is enforced core-side via the forwarded session-token header, and any failure (error or success=false) collapses to an empty history at the gateway. |
| WebAuthenticatePlayer | [WebAuthenticatePlayerRequest](#holomush-web-v1-WebAuthenticatePlayerRequest) | [WebAuthenticatePlayerResponse](#holomush-web-v1-WebAuthenticatePlayerResponse) | WebAuthenticatePlayer validates username/password and returns the player&#39;s character roster. Proxies to CoreService.AuthenticatePlayer; on success the gateway translates the core session token into a Set-Cookie signal. First runs the cookie-collision gate, short-circuiting with ALREADY_AUTHENTICATED if a valid session cookie is already present. |
| WebSelectCharacter | [WebSelectCharacterRequest](#holomush-web-v1-WebSelectCharacterRequest) | [WebSelectCharacterResponse](#holomush-web-v1-WebSelectCharacterResponse) | WebSelectCharacter binds a character to a new or reattached game session. Proxies to CoreService.SelectCharacter using the session token read from the X-Session-Token cookie header; returns the resulting session_id. |
| WebCreatePlayer | [WebCreatePlayerRequest](#holomush-web-v1-WebCreatePlayerRequest) | [WebCreatePlayerResponse](#holomush-web-v1-WebCreatePlayerResponse) | WebCreatePlayer registers a new player account. Proxies to CoreService.CreatePlayer; on success the gateway sets the session cookie. Runs the cookie-collision gate first (ALREADY_AUTHENTICATED short-circuit). |
| WebCreateGuest | [WebCreateGuestRequest](#holomush-web-v1-WebCreateGuestRequest) | [WebCreateGuestResponse](#holomush-web-v1-WebCreateGuestResponse) | WebCreateGuest provisions an ephemeral guest player and character. Proxies to CoreService.CreateGuest; on success the gateway sets a session cookie whose MaxAge matches the guest session&#39;s shorter TTL. Runs the cookie-collision gate first. |
| WebCreateCharacter | [WebCreateCharacterRequest](#holomush-web-v1-WebCreateCharacterRequest) | [WebCreateCharacterResponse](#holomush-web-v1-WebCreateCharacterResponse) | WebCreateCharacter adds a character to the authenticated player. Proxies to CoreService.CreateCharacter using the cookie-derived session token. |
| WebListCharacters | [WebListCharactersRequest](#holomush-web-v1-WebListCharactersRequest) | [WebListCharactersResponse](#holomush-web-v1-WebListCharactersResponse) | WebListCharacters returns the authenticated player&#39;s character roster. Proxies to CoreService.ListCharacters; an RPC failure is surfaced as CodeUnauthenticated (session expired or invalid). |
| WebLogout | [WebLogoutRequest](#holomush-web-v1-WebLogoutRequest) | [WebLogoutResponse](#holomush-web-v1-WebLogoutResponse) | WebLogout ends the player session and clears the session cookie. Proxies to CoreService.Logout (best-effort) when a token is present, then always emits the cookie-clear signal regardless of the RPC outcome. |
| WebRequestPasswordReset | [WebRequestPasswordResetRequest](#holomush-web-v1-WebRequestPasswordResetRequest) | [WebRequestPasswordResetResponse](#holomush-web-v1-WebRequestPasswordResetResponse) | WebRequestPasswordReset initiates the email-based reset flow. Proxies to CoreService.RequestPasswordReset; to avoid leaking account existence the gateway reports success=true even when the underlying RPC errors. |
| WebConfirmPasswordReset | [WebConfirmPasswordResetRequest](#holomush-web-v1-WebConfirmPasswordResetRequest) | [WebConfirmPasswordResetResponse](#holomush-web-v1-WebConfirmPasswordResetResponse) | WebConfirmPasswordReset completes a reset using the emailed token and a new password. Proxies to CoreService.ConfirmPasswordReset. |
| WebCheckSession | [WebCheckSessionRequest](#holomush-web-v1-WebCheckSessionRequest) | [WebCheckSessionResponse](#holomush-web-v1-WebCheckSessionResponse) | WebCheckSession validates the player session carried in the cookie and returns the player identity plus character roster, or a CodeUnauthenticated error. Proxies to CoreService.CheckPlayerSession; the web client&#39;s authed layout uses this to gate page loads. |
| WebGetContent | [WebGetContentRequest](#holomush-web-v1-WebGetContentRequest) | [WebGetContentResponse](#holomush-web-v1-WebGetContentResponse) | WebGetContent fetches a single content-store item by exact key. Proxies to ContentService.GetContent (a separate gRPC service, NOT CoreService); public, no auth. Returns CodeUnimplemented if the gateway was built without a content client. |
| WebListContent | [WebListContentRequest](#holomush-web-v1-WebListContentRequest) | [WebListContentResponse](#holomush-web-v1-WebListContentResponse) | WebListContent lists content-store items under a key prefix. Proxies to ContentService.ListContent (NOT CoreService); public, no auth. Returns CodeUnimplemented if no content client is configured. |
| WebQueryStreamHistory | [WebQueryStreamHistoryRequest](#holomush-web-v1-WebQueryStreamHistoryRequest) | [WebQueryStreamHistoryResponse](#holomush-web-v1-WebQueryStreamHistoryResponse) | WebQueryStreamHistory reads paginated event history for the web client. Proxies to CoreService.QueryStreamHistory — authorization is enforced by core. |
| WebListSessionStreams | [WebListSessionStreamsRequest](#holomush-web-v1-WebListSessionStreamsRequest) | [WebListSessionStreamsResponse](#holomush-web-v1-WebListSessionStreamsResponse) | WebListSessionStreams returns the stream names the session is subscribed to. Proxies to CoreService.ListSessionStreams — authorization is enforced by core. Used by the web client to enumerate streams for reload-backfill. |
| WebListPlayerSessions | [WebListPlayerSessionsRequest](#holomush-web-v1-WebListPlayerSessionsRequest) | [WebListPlayerSessionsResponse](#holomush-web-v1-WebListPlayerSessionsResponse) | WebListPlayerSessions returns the caller&#39;s active PlayerSessions (the device/tab login records, distinct from in-game game sessions), each flagged is_current for the calling session. Proxies to CoreService.ListPlayerSessions; the caller is identified via the X-Session-Token cookie header injected by CookieMiddleware — there is no token field in the request body. |
| WebRevokePlayerSession | [WebRevokePlayerSessionRequest](#holomush-web-v1-WebRevokePlayerSessionRequest) | [WebRevokePlayerSessionResponse](#holomush-web-v1-WebRevokePlayerSessionResponse) | WebRevokePlayerSession revokes one of the caller&#39;s PlayerSessions by id. Proxies to CoreService.RevokePlayerSession; caller identity comes from the X-Session-Token cookie header. |
| WebRevokeOtherPlayerSessions | [WebRevokeOtherPlayerSessionsRequest](#holomush-web-v1-WebRevokeOtherPlayerSessionsRequest) | [WebRevokeOtherPlayerSessionsResponse](#holomush-web-v1-WebRevokeOtherPlayerSessionsResponse) | WebRevokeOtherPlayerSessions revokes all of the caller&#39;s PlayerSessions except the current one (&#34;log out everywhere else&#34;). Proxies to CoreService.RevokeOtherPlayerSessions; caller identity comes from the X-Session-Token cookie header. |
| WebListFocusPresence | [WebListFocusPresenceRequest](#holomush-web-v1-WebListFocusPresenceRequest) | [WebListFocusPresenceResponse](#holomush-web-v1-WebListFocusPresenceResponse) | WebListFocusPresence returns the presence snapshot for the session&#39;s current focus context (location or scene). Proxies to CoreService.ListFocusPresence — authorization is enforced by core. player_session_token is read from the HTTP cookie by gateway middleware. |
| WebListCommands | [WebListCommandsRequest](#holomush-web-v1-WebListCommandsRequest) | [WebListCommandsResponse](#holomush-web-v1-WebListCommandsResponse) | WebListCommands returns the recognized-command set &#43; alias map for the session&#39;s character, for the composer&#39;s command chip. Proxies to CoreService.ListAvailableCommands; player_session_token is read from the cookie by gateway middleware. |

 



<a name="holomush_world_v1_world-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/world/v1/world.proto



<a name="holomush-world-v1-CharacterInfo"></a>

### CharacterInfo
CharacterInfo carries the public attributes of a player character. A
character is the in-game entity controlled by a player account. One player
may have multiple characters. The location_id is empty when the character is
not currently placed in the world (i.e., not logged in or not yet in a
location).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | ULID of the character, generated by idgen.New() at creation time. |
| player_id | [string](#string) |  | ULID of the player account that owns this character. |
| name | [string](#string) |  | Human-readable display name used for emotes, location descriptions, and character lists. |
| description | [string](#string) |  | Prose description shown when another character looks at this character. |
| location_id | [string](#string) |  | ULID of the location where the character is currently placed, or empty string if the character is not in the world. Corresponds to the nullable world.Character.LocationID field in internal/world/character.go. |






<a name="holomush-world-v1-ExitInfo"></a>

### ExitInfo
ExitInfo carries the public attributes of a directional connection between
two locations. An exit has a source location (from_location_id) and a
destination (to_location_id). When bidirectional is true, traversing the
exit in the reverse direction uses return_name as the visible direction
label. The locked field reflects the current lock state; the lock mechanism
details (key, password, condition) are not exposed through this RPC surface.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | ULID of the exit, generated by idgen.New() at creation time. |
| name | [string](#string) |  | Direction or display name for the exit as seen from from_location_id (e.g., &#34;north&#34;, &#34;out&#34;). |
| from_location_id | [string](#string) |  | ULID of the location from which this exit originates. |
| to_location_id | [string](#string) |  | ULID of the location this exit leads to. |
| bidirectional | [bool](#bool) |  | Whether a corresponding reverse exit exists. When true, return_name holds the direction label used when travelling from to_location_id back to from_location_id. |
| return_name | [string](#string) |  | Direction label for the reverse leg of a bidirectional exit (e.g., &#34;south&#34;). Empty string when bidirectional is false. |
| locked | [bool](#bool) |  | Whether the exit is currently locked. When locked, movement through this exit is blocked unless the character satisfies the lock condition evaluated by the movement subsystem. |






<a name="holomush-world-v1-GetCharacterRequest"></a>

### GetCharacterRequest
GetCharacterRequest identifies a character to fetch and the querying
character for access-control evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| subject_id | [string](#string) |  | ULID of the character performing the query. Passed to the ABAC engine as subject &#34;character:&lt;subject_id&gt;&#34; to check the &#34;read&#34; action on the character resource. |
| character_id | [string](#string) |  | ULID of the character to retrieve. Must be a valid strict ULID; malformed values result in codes.InvalidArgument. |






<a name="holomush-world-v1-GetCharacterResponse"></a>

### GetCharacterResponse
GetCharacterResponse wraps the character returned by GetCharacter. The
character field is always populated on a successful response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character | [CharacterInfo](#holomush-world-v1-CharacterInfo) |  | The requested character. |






<a name="holomush-world-v1-GetLocationRequest"></a>

### GetLocationRequest
GetLocationRequest identifies a location to fetch and the character
performing the lookup for access-control evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| subject_id | [string](#string) |  | ULID of the character performing the query. Passed to the ABAC engine as subject &#34;character:&lt;subject_id&gt;&#34; to check the &#34;read&#34; action on the location resource. |
| location_id | [string](#string) |  | ULID of the location to retrieve. Must be a valid strict ULID; malformed values result in codes.InvalidArgument. |






<a name="holomush-world-v1-GetLocationResponse"></a>

### GetLocationResponse
GetLocationResponse wraps the location returned by GetLocation. The location
field is always populated on a successful response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location | [LocationInfo](#holomush-world-v1-LocationInfo) |  | The requested location. |






<a name="holomush-world-v1-ListCharactersAtLocationRequest"></a>

### ListCharactersAtLocationRequest
ListCharactersAtLocationRequest identifies the location to query and the
character performing the query for access-control evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| subject_id | [string](#string) |  | ULID of the character performing the query. Passed to the ABAC engine as subject &#34;character:&lt;subject_id&gt;&#34; to check the &#34;list_characters&#34; action on the location resource (per ADR #76 decomposition). |
| location_id | [string](#string) |  | ULID of the location whose character roster is requested. Must be a valid strict ULID; malformed values result in codes.InvalidArgument. |






<a name="holomush-world-v1-ListCharactersAtLocationResponse"></a>

### ListCharactersAtLocationResponse
ListCharactersAtLocationResponse carries all characters currently placed at
the requested location. The characters list is empty (not absent) when no
characters are present; the field is never null.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterInfo](#holomush-world-v1-CharacterInfo) | repeated | Characters currently placed at the queried location. Order is not guaranteed; callers that need a stable display order should sort by name. |






<a name="holomush-world-v1-ListExitsRequest"></a>

### ListExitsRequest
ListExitsRequest identifies the location whose exits are requested and the
character performing the query for access-control evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| subject_id | [string](#string) |  | ULID of the character performing the query. Passed to the ABAC engine as subject &#34;character:&lt;subject_id&gt;&#34; to check the &#34;read&#34; action on the location resource. |
| location_id | [string](#string) |  | ULID of the location whose exits are requested. Must be a valid strict ULID; malformed values result in codes.InvalidArgument. |






<a name="holomush-world-v1-ListExitsResponse"></a>

### ListExitsResponse
ListExitsResponse carries all exits originating from the requested location.
The exits list is empty (not absent) when the location has no exits; the
field is never null.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| exits | [ExitInfo](#holomush-world-v1-ExitInfo) | repeated | Exits originating from the queried location. Order is not guaranteed; callers that need a stable display order should sort by name. |






<a name="holomush-world-v1-LocationInfo"></a>

### LocationInfo
LocationInfo carries the public attributes of a world location. A location
is a named place that can contain characters, objects, and exits. The type
field distinguishes persistent grid locations from temporary scene locations and
instanced copies. The owner_id is empty for unowned locations; ownership
affects exit visibility rules evaluated server-side.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | ULID of the location, generated by idgen.New() at creation time. |
| name | [string](#string) |  | Human-readable display name shown in location descriptions and exit listings. |
| description | [string](#string) |  | Prose description shown to characters entering or looking at the location. |
| type | [string](#string) |  | Spatial category of the location. One of &#34;persistent&#34; (permanent grid location), &#34;scene&#34; (ephemeral RP space cloned from a persistent location), or &#34;instance&#34; (private copy). Corresponds to world.LocationType in internal/world/location.go. |
| owner_id | [string](#string) |  | ULID of the player account that owns this location, or empty string if the location is unowned. Ownership influences exit visibility when the exit&#39;s visibility is set to &#34;owner&#34;. |





 

 

 


<a name="holomush-world-v1-WorldService"></a>

### WorldService
WorldService provides read-only world model queries for binary plugins.
It is served on an in-process gRPC connection registered in the plugin
service registry as &#34;holomush.world.v1.WorldService&#34; (see
internal/plugin/setup/world_conn.go::newWorldInProcessConn). Every RPC
enforces ABAC by passing subject_id through world.Service, which delegates
to the configured access.PolicyEngine before touching any repository.
Errors that indicate missing records map to codes.NotFound; denied access
maps to codes.PermissionDenied; all other failures map to codes.Internal
with no internal detail leaked to callers.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetLocation | [GetLocationRequest](#holomush-world-v1-GetLocationRequest) | [GetLocationResponse](#holomush-world-v1-GetLocationResponse) | GetLocation fetches a single location by ULID. The caller must hold the &#34;read&#34; permission on the location resource. Returns codes.NotFound if the location does not exist and codes.PermissionDenied if access is denied. |
| GetCharacter | [GetCharacterRequest](#holomush-world-v1-GetCharacterRequest) | [GetCharacterResponse](#holomush-world-v1-GetCharacterResponse) | GetCharacter fetches a single character by ULID. The caller must hold the &#34;read&#34; permission on the character resource. Returns codes.NotFound if the character does not exist and codes.PermissionDenied if access is denied. |
| ListCharactersAtLocation | [ListCharactersAtLocationRequest](#holomush-world-v1-ListCharactersAtLocationRequest) | [ListCharactersAtLocationResponse](#holomush-world-v1-ListCharactersAtLocationResponse) | ListCharactersAtLocation returns all characters whose current location matches location_id. The caller must hold the &#34;list_characters&#34; permission on the location resource (action=list_characters, resource=location:&lt;id&gt;, per ADR #76 compound-resource decomposition). Returns an empty list when no characters are present; never returns codes.NotFound for an empty location. |
| ListExits | [ListExitsRequest](#holomush-world-v1-ListExitsRequest) | [ListExitsResponse](#holomush-world-v1-ListExitsResponse) | ListExits returns all exits originating from a location. The caller must hold the &#34;read&#34; permission on the location resource. Returns an empty list when the location has no exits; never returns codes.NotFound for an empty exit set. |

 



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

