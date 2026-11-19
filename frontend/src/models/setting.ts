import { ObjectUtil } from '@/utils';
import type { SubProfileMode, SubHostSortMode } from '@/schemas/setting';

export class AllSetting {
  webListen = '';
  webDomain = '';
  webPort = 2053;
  webCertFile = '';
  webKeyFile = '';
  webBasePath = '/';
  sessionMaxAge = 360;
  trustedProxyCIDRs = '127.0.0.1/32,::1/128';
  realityScanCandidates =
    'www.cloudflare.com:443,www.microsoft.com:443,www.amazon.com:443,aws.amazon.com:443,www.samsung.com:443,www.nvidia.com:443,www.amd.com:443,www.intel.com:443,www.sony.com:443,dl.google.com:443';
  ipLimitAllowlist = '';
  panelOutbound = '';
  pageSize = 25;
  expireDiff = 0;
  trafficDiff = 0;
  remarkTemplate = '{{INBOUND}}-{{EMAIL}}|📊{{TRAFFIC_LEFT}}|⏳{{DAYS_LEFT}}D';
  subShowIdentityOnAllLinks = false;
  subInfoNodeEnable = false;
  subHostSortMode: SubHostSortMode = 'inbound-first';
  subCalendarExpireInclusive = false;
  subExpiredTemplate = '⛔ {{EMAIL}} | Expired: {{EXPIRE_DATE}}';
  subTrafficDepletedTemplate =
    '🚫 {{EMAIL}} | Traffic Depleted | {{TRAFFIC_USED}}/{{TRAFFIC_TOTAL}}';
  datepicker: 'gregorian' | 'jalalian' = 'gregorian';
  twoFactorEnable = false;
  twoFactorToken = '';
  xrayTemplateConfig = '';
  subEnable = true;
  subJsonEnable = false;
  subJsonAutoDetect = false;
  subJsonAlwaysArray = false;
  subJsonUserAgentRegex = '';
  subClashAutoDetect = false;
  subClashUserAgentRegex = '';
  subTitle = '';
  subSupportUrl = '';
  subProfileMode: SubProfileMode = 'none';
  subProfileUrl = '';
  subAnnounce = '';
  subListen = '';
  subPort = 2096;
  subPath = '/sub/';
  subJsonPath = '/json/';
  subClashEnable = false;
  subClashPath = '/clash/';
  subDomain = '';
  externalTrafficInformEnable = false;
  externalTrafficInformURI = '';
  restartXrayOnClientDisable = true;
  subCertFile = '';
  subKeyFile = '';
  externalSubUserAgent = 'v2rayNG/1.8.5';
  subUpdates = 12;
  subEncrypt = true;
  subURI = '';
  subJsonURI = '';
  subClashURI = '';
  subClashEnableRouting = false;
  subClashRules = '';
  subClashRemoteDNS = false;
  subJsonMux = '';
  subJsonRules = '';
  subJsonRoutingRules = '';
  subJsonDns = '';
  subJsonFinalMask = '';
  subJsonObservatory = '';
  subThemeDir = '';

  timeLocation = 'Local';

  hasTwoFactorToken = false;
  hasApiToken = false;

  constructor(data?: unknown) {
    if (data != null) {
      ObjectUtil.cloneProps(this, data);
    }
    // Legacy settings with a custom URL retain it until an explicit mode is saved.
    if (
      typeof data === 'object' &&
      data !== null &&
      (!('subProfileMode' in data) || data.subProfileMode === undefined) &&
      this.subProfileUrl.trim() !== ''
    ) {
      this.subProfileMode = 'custom';
    }
  }

  equals(other: AllSetting): boolean {
    return ObjectUtil.equals(this, other);
  }
}
