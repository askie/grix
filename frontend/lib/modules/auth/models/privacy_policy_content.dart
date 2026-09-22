class PrivacyPolicySectionData {
  const PrivacyPolicySectionData({
    required this.titleKey,
    this.paragraphKeys = const <String>[],
    this.bulletKeys = const <String>[],
  });

  final String titleKey;
  final List<String> paragraphKeys;
  final List<String> bulletKeys;
}

/// In-app privacy policy body keys. English text mirrors
/// `backend/internal/publicsite/pages/privacy-policy.html`.
class PrivacyPolicyContent {
  static const List<PrivacyPolicySectionData> sections =
      <PrivacyPolicySectionData>[
        PrivacyPolicySectionData(
          titleKey: 'privacy_policy_section_data_categories_title',
          bulletKeys: <String>[
            'privacy_policy_section_data_categories_bullet_1',
            'privacy_policy_section_data_categories_bullet_2',
            'privacy_policy_section_data_categories_bullet_3',
            'privacy_policy_section_data_categories_bullet_4',
          ],
        ),
        PrivacyPolicySectionData(
          titleKey: 'privacy_policy_section_why_title',
          bulletKeys: <String>[
            'privacy_policy_section_why_bullet_1',
            'privacy_policy_section_why_bullet_2',
            'privacy_policy_section_why_bullet_3',
            'privacy_policy_section_why_bullet_4',
            'privacy_policy_section_why_bullet_5',
          ],
        ),
        PrivacyPolicySectionData(
          titleKey: 'privacy_policy_section_retention_title',
          bulletKeys: <String>[
            'privacy_policy_section_retention_bullet_1',
            'privacy_policy_section_retention_bullet_2',
            'privacy_policy_section_retention_bullet_3',
          ],
        ),
        PrivacyPolicySectionData(
          titleKey: 'privacy_policy_section_controls_title',
          bulletKeys: <String>[
            'privacy_policy_section_controls_bullet_1',
            'privacy_policy_section_controls_bullet_2',
            'privacy_policy_section_controls_bullet_3',
          ],
        ),
        PrivacyPolicySectionData(
          titleKey: 'privacy_policy_section_sharing_title',
          paragraphKeys: <String>[
            'privacy_policy_section_sharing_body_1',
            'privacy_policy_section_sharing_body_2',
          ],
        ),
      ];
}
