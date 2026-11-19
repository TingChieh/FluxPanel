import { z } from 'zod';

const port = z.number().int().min(1).max(65535);
const nonNegativeInt = z.number().int().min(0);
const absolutePath = z.string().regex(/^\//, 'pages.settings.validation.pathLeadingSlash');

export const SubProfileModeSchema = z.enum(['none', 'builtin', 'custom']);
export type SubProfileMode = z.infer<typeof SubProfileModeSchema>;

export const SubHostSortModeSchema = z.enum(['inbound-first', 'hosts-first']);
export type SubHostSortMode = z.infer<typeof SubHostSortModeSchema>;

export const AllSettingSchema = z
  .object({
    webListen: z.string().optional(),
    webDomain: z.string().optional(),
    webPort: port.optional(),
    webCertFile: z.string().optional(),
    webKeyFile: z.string().optional(),
    webBasePath: absolutePath.optional(),
    sessionMaxAge: z.number().int().min(1).max(525600).optional(),
    trustedProxyCIDRs: z.string().optional(),
    realityScanCandidates: z.string().optional(),
    ipLimitAllowlist: z.string().optional(),
    panelOutbound: z.string().optional(),
    pageSize: z.number().int().min(0).max(1000).optional(),
    expireDiff: nonNegativeInt.optional(),
    trafficDiff: nonNegativeInt.max(100).optional(),
    remarkTemplate: z.string().optional(),
    subShowIdentityOnAllLinks: z.boolean().optional(),
    subInfoNodeEnable: z.boolean().optional(),
    subHostSortMode: SubHostSortModeSchema.optional(),
    subCalendarExpireInclusive: z.boolean().optional(),
    subExpiredTemplate: z.string().optional(),
    subTrafficDepletedTemplate: z.string().optional(),
    datepicker: z.enum(['gregorian', 'jalalian']).optional(),
    twoFactorEnable: z.boolean().optional(),
    twoFactorToken: z.string().optional(),
    xrayTemplateConfig: z.string().optional(),
    subEnable: z.boolean().optional(),
    subJsonEnable: z.boolean().optional(),
    subJsonAutoDetect: z.boolean().optional(),
    subJsonAlwaysArray: z.boolean().optional(),
    subJsonUserAgentRegex: z.string().max(2048).optional(),
    subClashAutoDetect: z.boolean().optional(),
    subClashUserAgentRegex: z.string().max(2048).optional(),
    subTitle: z.string().optional(),
    subSupportUrl: z.string().optional(),
    subProfileMode: SubProfileModeSchema.optional(),
    subProfileUrl: z.string().optional(),
    subAnnounce: z.string().optional(),
    subListen: z.string().optional(),
    subPort: port.optional(),
    subPath: absolutePath.optional(),
    subJsonPath: absolutePath.optional(),
    subClashEnable: z.boolean().optional(),
    subClashPath: absolutePath.optional(),
    subDomain: z.string().optional(),
    externalTrafficInformEnable: z.boolean().optional(),
    externalTrafficInformURI: z.string().optional(),
    restartXrayOnClientDisable: z.boolean().optional(),
    subCertFile: z.string().optional(),
    subKeyFile: z.string().optional(),
    externalSubUserAgent: z.string().max(512).optional(),
    subUpdates: z.number().int().min(0).max(525600).optional(),
    subEncrypt: z.boolean().optional(),
    subURI: z.string().optional(),
    subJsonURI: z.string().optional(),
    subClashURI: z.string().optional(),
    subClashEnableRouting: z.boolean().optional(),
    subClashRules: z.string().optional(),
    subClashRemoteDNS: z.boolean().optional(),
    subJsonMux: z.string().optional(),
    subJsonRules: z.string().optional(),
    subJsonRoutingRules: z.string().optional(),
    subJsonDns: z.string().optional(),
    subJsonFinalMask: z.string().optional(),
    subJsonObservatory: z.string().optional(),
    timeLocation: z.string().optional(),
    hasTwoFactorToken: z.boolean().optional(),
    hasApiToken: z.boolean().optional(),
  })
  .loose();

export type AllSettingInput = z.infer<typeof AllSettingSchema>;

// Existing installations can contain regex values saved before the backend
// enforced its 2,048-character limit. Accept those values when reading so the
// settings page can display and let users correct them, while keeping the
// stricter schema above for outgoing updates.
export const AllSettingResponseSchema = AllSettingSchema.extend({
  subJsonUserAgentRegex: z.string().optional(),
  subClashUserAgentRegex: z.string().optional(),
});

export const FactoryDefaultsSchema = z.record(z.string(), z.string());

export type FactoryDefaults = z.infer<typeof FactoryDefaultsSchema>;
