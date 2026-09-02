class UserAgreementSectionData {
  const UserAgreementSectionData({
    required this.titleKey,
    this.paragraphKeys = const <String>[],
    this.bulletKeys = const <String>[],
  });

  final String titleKey;
  final List<String> paragraphKeys;
  final List<String> bulletKeys;
}

class UserAgreementContent {
  static const List<UserAgreementSectionData> sections =
      <UserAgreementSectionData>[
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_scope_title',
          paragraphKeys: <String>[
            'user_agreement_section_scope_body_1',
            'user_agreement_section_scope_body_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_account_title',
          paragraphKeys: <String>[
            'user_agreement_section_account_body_1',
            'user_agreement_section_account_body_2',
          ],
          bulletKeys: <String>[
            'user_agreement_section_account_bullet_1',
            'user_agreement_section_account_bullet_2',
            'user_agreement_section_account_bullet_3',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_behavior_title',
          paragraphKeys: <String>['user_agreement_section_behavior_body_1'],
          bulletKeys: <String>[
            'user_agreement_section_behavior_bullet_1',
            'user_agreement_section_behavior_bullet_2',
            'user_agreement_section_behavior_bullet_3',
            'user_agreement_section_behavior_bullet_4',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_content_title',
          paragraphKeys: <String>[
            'user_agreement_section_content_body_1',
            'user_agreement_section_content_body_2',
          ],
          bulletKeys: <String>[
            'user_agreement_section_content_bullet_1',
            'user_agreement_section_content_bullet_2',
            'user_agreement_section_content_bullet_3',
            'user_agreement_section_content_bullet_4',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_privacy_title',
          paragraphKeys: <String>[
            'user_agreement_section_privacy_body_1',
            'user_agreement_section_privacy_body_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_minor_title',
          paragraphKeys: <String>['user_agreement_section_minor_body_1'],
          bulletKeys: <String>[
            'user_agreement_section_minor_bullet_1',
            'user_agreement_section_minor_bullet_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_service_title',
          paragraphKeys: <String>[
            'user_agreement_section_service_body_1',
            'user_agreement_section_service_body_2',
          ],
          bulletKeys: <String>['user_agreement_section_service_bullet_1'],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_fee_title',
          paragraphKeys: <String>['user_agreement_section_fee_body_1'],
          bulletKeys: <String>[
            'user_agreement_section_fee_bullet_1',
            'user_agreement_section_fee_bullet_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_liability_title',
          paragraphKeys: <String>[
            'user_agreement_section_liability_body_1',
            'user_agreement_section_liability_body_2',
            'user_agreement_section_liability_body_3',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_ip_title',
          paragraphKeys: <String>['user_agreement_section_ip_body_1'],
          bulletKeys: <String>[
            'user_agreement_section_ip_bullet_1',
            'user_agreement_section_ip_bullet_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_dispute_title',
          paragraphKeys: <String>[
            'user_agreement_section_dispute_body_1',
            'user_agreement_section_dispute_body_2',
          ],
        ),
        UserAgreementSectionData(
          titleKey: 'user_agreement_section_update_title',
          paragraphKeys: <String>[
            'user_agreement_section_update_body_1',
            'user_agreement_section_update_body_2',
          ],
        ),
      ];
}
